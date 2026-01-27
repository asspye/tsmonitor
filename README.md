# TSMonitor

MPEG-TS Stream Monitoring with Prometheus Integration

## 🎯 Overview

TSMonitor is a high-performance Go application that monitors MPEG-TS multicast streams using TSDuck tools and exports metrics to Prometheus.

## ✨ Features

- **Real-time monitoring** of ~200 MPEG-TS multicast streams
- **Comprehensive metrics**:
  - Stream status (online/offline)
  - Bitrate (total and net)
  - PID information (video, audio, data)
  - Service information (name, provider, type)
  - Continuity Counter (CC) errors
- **Prometheus integration** for metrics export
- **Grafana dashboards** for visualization
- **Efficient streaming architecture** with sliding window buffer
- **Automatic restart** on stream failures

## 🏗️ Architecture
```
┌─────────────┐
│  Multicast  │
│   Streams   │ (233.198.134.*)
└──────┬──────┘
       │
       ▼
┌─────────────────────────────┐
│      TSMonitor (Go)         │
│                             │
│  ┌────────────────────┐     │
│  │ StreamingRunner    │     │
│  │  (per stream)      │     │
│  │                    │     │
│  │  ┌──────────┐      │     │
│  │  │   tsp    │      │     │
│  │  │ (TSDuck) │      │     │
│  │  └──────────┘      │     │
│  │        │           │     │
│  │        ▼           │     │
│  │  ┌──────────┐      │     │
│  │  │  Parser  │      │     │
│  │  └──────────┘      │     │
│  └────────┬───────────┘     │
│           │                 │
│           ▼                 │
│  ┌────────────────────┐     │
│  │ Prometheus Exporter│     │
│  └────────────────────┘     │
└──────────────┬──────────────┘
               │
               ▼
        ┌─────────────┐
        │ Prometheus  │
        └──────┬──────┘
               │
               ▼
        ┌─────────────┐
        │   Grafana   │
        └─────────────┘
```

## 📋 Prerequisites

- Go 1.23+
- TSDuck tools installed
- Multicast network access
- Prometheus (for metrics collection)
- Grafana (for visualization)

## 🚀 Installation

### 1. Install TSDuck
```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install tsduck
```

### 2. Clone and Build
```bash
git clone https://github.com/YOUR_USERNAME/tsmonitor.git
cd tsmonitor

# Build
go build -o bin/tsmonitor ./cmd/tsmonitor
```

### 3. Configure
```bash
# Copy example config
cp config.yaml.example config.yaml

# Edit config
nano config.yaml
```

Example configuration:
```yaml
interface: "172.22.2.154"
metrics_port: 9090
timeout: 10s

streams:
  - url: "233.198.134.1:3333"
    description: "Stream Name| Provider| HD| multicast| ID001"
  
  - url: "233.198.134.2:3333"
    description: "Stream Name 2| Provider| SD| multicast| ID002"
```

## 🎮 Usage

### Run manually
```bash
./bin/tsmonitor config.yaml
```

### Run as systemd service
```bash
# Copy service file
sudo cp deploy/tsmonitor.service /etc/systemd/system/

# Enable and start
sudo systemctl enable tsmonitor
sudo systemctl start tsmonitor
sudo systemctl status tsmonitor
```

## 📊 Metrics

TSMonitor exports the following Prometheus metrics:

### Stream Status
```
ts_stream_status{stream, description} = 1 (online) / 0 (offline)
```

### Bitrate
```
ts_stream_bitrate_bps{stream, description, type="total|net"}
```

### PID Count
```
ts_stream_pid_count{stream, description, type="video|audio|data|other"}
```

### PID Information
```
ts_stream_pid_info{stream, description, pid, type, codec, language} = 1
```

### Service Information
```
ts_stream_service_info{stream, description, service_name, provider, service_type} = 1
```

### CC Errors
```
ts_stream_cc_errors_total{stream, description, pid}
```

## 📈 Grafana Dashboards

Import dashboards from `grafana-dashboards/`:

1. **Overview Dashboard** (`ts-stream-overview.json`)
   - Stream status grid
   - Bitrate charts
   - CC errors monitoring

2. **Details Dashboard** (`ts-stream-details.json`)
   - Per-stream detailed view
   - PID information
   - Bitrate breakdown

## 🔧 Development

### Project Structure
```
tsmonitor/
├── cmd/
│   ├── tsmonitor/         # Main application
│   ├── test_streaming/    # Streaming runner test
│   └── test_config/       # Config loader test
├── internal/
│   ├── config/            # Configuration management
│   ├── metrics/           # Prometheus exporter
│   ├── monitor/           # Orchestrator
│   └── tsp/              # TSP runner and parser
├── grafana-dashboards/    # Grafana dashboard JSONs
├── deploy/                # Deployment files
├── go.mod
├── go.sum
├── config.yaml.example
└── README.md
```

### Run Tests
```bash
go test ./...
```

### Build
```bash
go build -o bin/tsmonitor ./cmd/tsmonitor
```

## 📝 Configuration

### Prometheus Scrape Config

Add to your `prometheus.yml`:
```yaml
scrape_configs:
  - job_name: 'tsmonitor'
    scrape_interval: 15s
    static_configs:
      - targets: ['172.22.2.154:9090']
        labels:
          instance: 'docker-otcnet'
          service: 'ts-streams'
```

## 🐛 Troubleshooting

### Check service status
```bash
sudo systemctl status tsmonitor
sudo journalctl -u tsmonitor -f
```

### Check metrics endpoint
```bash
curl http://localhost:9090/metrics
```

### Test single stream
```bash
./bin/test_streaming
```

## 📄 License

Private Project - All Rights Reserved

## 👥 Authors

- Vladimir Plaksin (@asspye)

## 🙏 Acknowledgments

- TSDuck - The MPEG Transport Stream Toolkit
- Prometheus - Monitoring system & time series database
- Grafana - Analytics & monitoring platform
