# Septentrio Exporter

A Prometheus exporter for Septentrio GNSS receivers. This application connects to configured Septentrio stations via TCP, parses their SBF (Septentrio Binary Format) data streams, and exposes relevant metrics for Prometheus scraping.

## Features

- **Multi-station support**: Monitor multiple receivers simultaneously.
- **SBF Parsing**: Efficiently parses binary streams for block IDs:
  - PVTGeodetic (4007)
  - ReceiverStatus (4014)
  - MeasEpoch (4027)
  - DiskStatus (4059)
  - QualityInd (4082)
  - RFStatus (4092)
- **Metrics**: Exports key metrics including:
  - Satellites tracked/used
  - CPU Load, Temperature, Uptime
  - Disk space usage
  - Signal & RF Quality indicators
  - Jamming status

## Configuration

The application requires a `config.yaml` file. An example configuration:

```yaml
server:
  port: 9100 # Port for Prometheus to scrape

stations:
  - name: "reference-station-1"
    host: "192.168.1.10"
    port: 28000
  - name: "rover-1"
    host: "192.168.1.11"
    port: 28000
```

## Usage

### Local Run

1. Ensure Go 1.25+ is installed.
2. Create `config.yaml`.
3. Run the application:

```bash
go run main.go
```

Metrics will be available at `http://localhost:9100/metrics`.

### Docker

#### Build

```bash
docker build -t septentrino-exporter .
```

#### Run

```bash
docker run -d \
  -p 9100:9100 \
  -v $(pwd)/config.yaml:/root/config.yaml \
  septentrino-exporter
```

## Metrics

| Metric Name | Description |
|---|---|
| `gnss_satellites_tracked_total` | Total satellites visible |
| `gnss_satellites_used_total` | Satellites used in solution |
| `gnss_jamming_status_code` | 0=None, 1=Warning, 2=Critical |
| `gnss_receiver_connected` | Connection status (1=Connected, 0=Disconnected) |
| `gnss_cpu_load_percent` | Receiver CPU Load (0-100) |
| `gnss_temperature_celsius` | Internal Temperature |
| `gnss_uptime_seconds` | Receiver Uptime |
| `gnss_disk_free_bytes` | Free internal disk space |
| `gnss_quality_overall` | Overall Quality Indicator (0-10) |
| `gnss_quality_signals` | GNSS Signal Quality (0-10) |
| `gnss_quality_rf` | RF Power Quality (0-10) |

## Development

The application uses the `github.com/prometheus/client_golang` library for metrics and standard `net` and `bufio` packages for TCP streaming and binary parsing.
