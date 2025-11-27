# Power Exporter

Battery metrics exporter for Linux systems. Exports battery information to Prometheus (scrape/push) and InfluxDB.

## Features

- Auto-discovers all batteries (BAT0, BAT1, etc.)
- Reads from `/sys/class/power_supply/BAT*/uevent`
- Multiple export targets can run simultaneously:
  - Prometheus metrics endpoint (scrape)
  - Prometheus Pushgateway
  - InfluxDB

## Metrics

| Metric | Description |
|--------|-------------|
| `battery_percentage` | Current charge level (0-100) |
| `battery_capacity_percent` | Battery health vs design capacity |
| `battery_charging` | 0=Discharging, 1=Charging, 2=Full, 3=Not charging |
| `battery_voltage_volts` | Current voltage |
| `battery_energy_wh` | Current energy in Wh |
| `battery_cycle_count` | Charge cycle count |

All metrics have a `battery` label (BAT0, BAT1, etc.)

## Installation

```bash
# Download from releases or build from source
go build -o power-exporter .

# Generate default config
./power-exporter -gc .power-exporter.yml

# Edit config
vim .power-exporter.yml

# Run
./power-exporter
```

## Usage

```bash
# Use default config (.power-exporter.yml)
./power-exporter

# Specify config file
./power-exporter -c /etc/power-exporter.yml

# Generate default config at specified path
./power-exporter -gc /etc/power-exporter.yml
```

## Systemd Installation

Install as a systemd service (requires root):

```bash
# Install with defaults
# Binary: /usr/local/bin/power-exporter
# Config: /usr/local/etc/power-exporter.yml
sudo ./power-exporter -install

# Install with custom paths
sudo ./power-exporter -install -bin /opt/power-exporter -config /etc/power-exporter.yml
```

This will:
- Copy binary to the specified location
- Create default config if it doesn't exist
- Create and enable systemd service
- Start the service

Manage the service:

```bash
sudo systemctl status power-exporter
sudo systemctl restart power-exporter
sudo journalctl -u power-exporter -f
```

## Configuration

Generate a default config file:

```bash
./power-exporter -gc .power-exporter.yml
```

See `power-exporter.yml.example` for all options.

## License

MIT License

Copyright (c) [2025] [https://github.com/coolerUA]

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.