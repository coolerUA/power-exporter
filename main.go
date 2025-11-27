package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Interval int `yaml:"interval"`

	Prometheus struct {
		Enabled bool   `yaml:"enabled"`
		Port    int    `yaml:"port"`
		Path    string `yaml:"path"`
	} `yaml:"prometheus"`

	Pushgateway struct {
		Enabled bool   `yaml:"enabled"`
		URL     string `yaml:"url"`
		Job     string `yaml:"job"`
	} `yaml:"pushgateway"`

	InfluxDB struct {
		Enabled bool   `yaml:"enabled"`
		URL     string `yaml:"url"`
		Token   string `yaml:"token"`
		Org     string `yaml:"org"`
		Bucket  string `yaml:"bucket"`
	} `yaml:"influxdb"`

	Host string `yaml:"host"`
}

type BatteryInfo struct {
	Name         string
	Status       string
	Present      bool
	Technology   string
	CycleCount   int
	VoltageNow   int
	EnergyFull   int
	EnergyNow    int
	EnergyDesign int
	Capacity     int
	Model        string
	Manufacturer string
	Serial       string
}

var (
	config     Config
	batteries  []string
	promGauges = make(map[string]map[string]*prometheus.GaugeVec)
)

func loadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &config)
}

func findBatteries() []string {
	var result []string
	entries, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		log.Printf("Error reading power_supply: %v", err)
		return result
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "BAT") {
			ueventPath := filepath.Join("/sys/class/power_supply", e.Name(), "uevent")
			if _, err := os.Stat(ueventPath); err == nil {
				result = append(result, e.Name())
			}
		}
	}
	return result
}

func readBatteryInfo(name string) (*BatteryInfo, error) {
	path := filepath.Join("/sys/class/power_supply", name, "uevent")
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info := &BatteryInfo{Name: name}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "POWER_SUPPLY_STATUS":
			info.Status = val
		case "POWER_SUPPLY_PRESENT":
			info.Present = val == "1"
		case "POWER_SUPPLY_TECHNOLOGY":
			info.Technology = val
		case "POWER_SUPPLY_CYCLE_COUNT":
			info.CycleCount, _ = strconv.Atoi(val)
		case "POWER_SUPPLY_VOLTAGE_NOW":
			info.VoltageNow, _ = strconv.Atoi(val)
		case "POWER_SUPPLY_ENERGY_FULL_DESIGN":
			info.EnergyDesign, _ = strconv.Atoi(val)
		case "POWER_SUPPLY_ENERGY_FULL":
			info.EnergyFull, _ = strconv.Atoi(val)
		case "POWER_SUPPLY_ENERGY_NOW":
			info.EnergyNow, _ = strconv.Atoi(val)
		case "POWER_SUPPLY_CAPACITY":
			info.Capacity, _ = strconv.Atoi(val)
		case "POWER_SUPPLY_MODEL_NAME":
			info.Model = val
		case "POWER_SUPPLY_MANUFACTURER":
			info.Manufacturer = val
		case "POWER_SUPPLY_SERIAL_NUMBER":
			info.Serial = val
		}
	}
	return info, nil
}

func initPrometheusMetrics() {
	for _, bat := range batteries {
		promGauges[bat] = map[string]*prometheus.GaugeVec{
			"percentage": prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "battery_percentage",
				Help: "Battery charge percentage",
			}, []string{"battery"}),
			"capacity": prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "battery_capacity_percent",
				Help: "Battery health/capacity compared to design",
			}, []string{"battery"}),
			"charging": prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "battery_charging",
				Help: "1 if charging, 0 if discharging, 2 if full",
			}, []string{"battery"}),
			"voltage": prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "battery_voltage_volts",
				Help: "Current battery voltage in volts",
			}, []string{"battery"}),
			"energy_now": prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "battery_energy_wh",
				Help: "Current energy in Wh",
			}, []string{"battery"}),
			"cycle_count": prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "battery_cycle_count",
				Help: "Battery cycle count",
			}, []string{"battery"}),
		}
	}
	// Register only once (first battery's gauges are shared)
	if len(batteries) > 0 {
		bat := batteries[0]
		for _, g := range promGauges[bat] {
			prometheus.MustRegister(g)
		}
	}
}

func updateMetrics() {
	interval := time.Duration(config.Interval) * time.Second
	if interval == 0 {
		interval = 10 * time.Second
	}

	var influxClient influxdb2.Client
	var influxWriteAPI api.WriteAPI
	if config.InfluxDB.Enabled {
		influxClient = influxdb2.NewClient(config.InfluxDB.URL, config.InfluxDB.Token)
		influxWriteAPI = influxClient.WriteAPI(config.InfluxDB.Org, config.InfluxDB.Bucket)
	}

	for {
		for _, batName := range batteries {
			info, err := readBatteryInfo(batName)
			if err != nil {
				log.Printf("Error reading %s: %v", batName, err)
				continue
			}

			percentage := float64(info.Capacity)
			capacityHealth := 100.0
			if info.EnergyDesign > 0 {
				capacityHealth = 100.0 * float64(info.EnergyFull) / float64(info.EnergyDesign)
			}
			// Status: 0=Discharging, 1=Charging, 2=Full, 3=Not charging
			charging := 0.0
			switch info.Status {
			case "Charging":
				charging = 1.0
			case "Full":
				charging = 2.0
			case "Not charging":
				charging = 3.0
			}
			voltage := float64(info.VoltageNow) / 1000000.0
			energyWh := float64(info.EnergyNow) / 1000000.0

			// Prometheus metrics (for both scrape and push)
			if config.Prometheus.Enabled || config.Pushgateway.Enabled {
				g := promGauges[batteries[0]]
				g["percentage"].WithLabelValues(batName).Set(percentage)
				g["capacity"].WithLabelValues(batName).Set(capacityHealth)
				g["charging"].WithLabelValues(batName).Set(charging)
				g["voltage"].WithLabelValues(batName).Set(voltage)
				g["energy_now"].WithLabelValues(batName).Set(energyWh)
				g["cycle_count"].WithLabelValues(batName).Set(float64(info.CycleCount))
			}

			// InfluxDB
			if config.InfluxDB.Enabled && influxWriteAPI != nil {
				p := influxdb2.NewPoint(
					"battery",
					map[string]string{
						"host":    config.Host,
						"battery": batName,
					},
					map[string]interface{}{
						"percentage":      percentage,
						"capacity_health": capacityHealth,
						"charging":        charging,
						"voltage":         voltage,
						"energy_wh":       energyWh,
						"cycle_count":     info.CycleCount,
						"status":          info.Status,
					},
					time.Now())
				influxWriteAPI.WritePoint(p)
			}
		}

		if config.InfluxDB.Enabled && influxWriteAPI != nil {
			influxWriteAPI.Flush()
		}

		// Pushgateway
		if config.Pushgateway.Enabled {
			job := config.Pushgateway.Job
			if job == "" {
				job = "power_exporter"
			}
			pusher := push.New(config.Pushgateway.URL, job).
				Grouping("host", config.Host)
			for _, g := range promGauges[batteries[0]] {
				pusher = pusher.Collector(g)
			}
			if err := pusher.Push(); err != nil {
				log.Printf("Pushgateway error: %v", err)
			}
		}

		time.Sleep(interval)
	}
}

func main() {
	configPath := flag.String("c", ".power-exporter.yml", "Path to config file")
	flag.Parse()

	if err := loadConfig(*configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	batteries = findBatteries()
	if len(batteries) == 0 {
		log.Fatal("No batteries found")
	}
	log.Printf("Found batteries: %v", batteries)

	if config.Prometheus.Enabled || config.Pushgateway.Enabled {
		initPrometheusMetrics()
	}

	go updateMetrics()

	if config.Prometheus.Enabled {
		path := config.Prometheus.Path
		if path == "" {
			path = "/metrics"
		}
		port := config.Prometheus.Port
		if port == 0 {
			port = 9273
		}
		http.Handle(path, promhttp.Handler())
		log.Printf("Prometheus metrics at :%d%s", port, path)
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	} else {
		// Keep running even without prometheus
		select {}
	}
}
