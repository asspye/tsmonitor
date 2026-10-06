# TSMonitor

MPEG-TS Stream Monitoring with Prometheus Integration

## 🎯 Overview

TSMonitor is a high-performance Go application that monitors MPEG-TS multicast streams using TSDuck tools and exports metrics to Prometheus.

## ✨ Features

- **One `tsp` process per stream** (~380 multicast streams), restarted automatically if it exits
- **Structured TSDuck output** — no text parsing of table dumps:
  - `analyze --json-line` every 5 s: bitrate (TS and per PID), PIDs with codec / language / resolution,
    services, CC errors per PID, transport errors (TEI), sync errors, PAT/PMT/SDT/NIT/CAT repetition intervals
  - `iat`: UDP inter-arrival time (mean, stddev, min, max) — **network jitter**
  - `pcrverify --input-synchronous`: PCR arrival jitter above a threshold — **PCR jitter**
  - `splicemonitor --all-commands --json-line`: **SCTE-35** splice commands and ad-break events
- **Event log** (JSON lines): SCTE-35, CC error start/end, PID and service changes, stream down/up — for Loki / Grafana
- **Host UDP drops** (`RcvbufErrors`) to tell local receive-buffer losses from network losses
- Metrics are built from the current state at scrape time — no stale series

## 🏗️ Architecture
```
multicast ──► tsp -I ip … -P iat -P pcrverify -P splicemonitor -P analyze -O drop   (one per stream)
                 │ JSON lines (AN:{…}, SCTE:{…}) + iat/pcrverify lines
                 ▼
           tsmonitor (Go) ── stream state ──► /metrics (Prometheus) ──► Grafana
                 │
                 └──► /var/log/tsmonitor/events.log ─► Alloy ──► Loki
```

## 📋 Prerequisites

- Go 1.23+
- TSDuck >= 3.40 (`iat` plugin; tested with 3.45-4798)
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
or install from site https://tsduck.io/docs/tsduck.html#_installing_tsduck

### 2. Clone and Build
```bash
git clone https://github.com/asspye/tsmonitor.git
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

See `config.yaml.example` for all options (interval, receive buffer, PCR jitter threshold, SCTE-35, event log).

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

All stream metrics have labels `stream` and `description`.

| Metric | Extra labels | Meaning |
|---|---|---|
| `ts_stream_status` | | 1 online / 0 offline (no analyze report for 3×interval or zero bitrate) |
| `ts_stream_bitrate_bps` | `type=total\|net` | TS bitrate; net = without null packets |
| `ts_stream_pid_count` | `type=video\|audio\|data\|other` | elementary streams by type |
| `ts_stream_pid_info` | `pid, type, codec, language, resolution` | elementary stream info (value 1) |
| `ts_stream_pid_bitrate_bps` | `pid, type` | bitrate of each elementary stream |
| `ts_stream_service_info` | `service_name, provider, service_type` | service info (value 1) |
| `ts_stream_cc_errors_total` | `pid` | continuity counter errors |
| `ts_stream_transport_errors_total` | | packets with transport_error_indicator |
| `ts_stream_sync_errors_total` | | packets with invalid sync byte |
| `ts_stream_table_interval_seconds` | `table=PAT\|PMT\|SDT\|NIT\|CAT, stat=avg\|max` | table repetition interval |
| `ts_stream_iat_seconds` | `stat=mean\|stddev\|min\|max` | UDP inter-arrival time (network jitter) |
| `ts_stream_pcr_jitter_exceeded_total` | | PCRs with arrival jitter above `pcr_jitter_max` |
| `ts_stream_pcr_jitter_max_seconds` | | largest such jitter in the last interval |
| `ts_stream_scte35_commands_total` | `command` | SCTE-35 commands, incl. `splice_null` heartbeats |
| `ts_stream_scte35_last_command_timestamp_seconds` | | last SCTE-35 section (inserter alive) |
| `ts_stream_scte35_events_total` | `direction=out\|in` | ad break start / return events that occurred |
| `ts_stream_scte35_last_event_timestamp_seconds` | `direction` | time of the last occurred event |
| `ts_stream_scte35_preroll_seconds` | `direction` | pre-roll of the last event (first announcement → event) |
| `ts_stream_scte35_break_active` | | 1 while an ad break is in progress |
| `ts_stream_scte35_break_duration_seconds` | | announced duration of the current / last break |
| `ts_stream_last_report_timestamp_seconds` | | last analyze report |
| `ts_stream_restarts_total` | | tsp restarts |
| `ts_stream_parse_errors_total` | | unparsable tsp output lines |
| `ts_host_udp_in_datagrams_total`, `ts_host_udp_in_errors_total`, `ts_host_udp_rcvbuf_errors_total` | | host UDP counters from `/proc/net/snmp` |

SCTE-35 metrics appear only for streams where SCTE-35 sections were seen.

### Event log

`event_log` (default in deploy: `/var/log/tsmonitor/events.log`) gets one JSON object per line.
Every record has `time`, `stream`, `description`, `kind` and a human-readable Russian `message`:

| kind | when |
|---|---|
| `cc_errors` | `phase=start`: first analyze interval with CC errors after a clean one (`window_start`, per-PID counts); `phase=end`: first clean interval (`duration_s`, `total`, per-PID totals) |
| `pids_changed` | elementary streams added / removed / changed codec, type, language or resolution (confirmed by 2 consecutive reports) |
| `service_changed` | service name / provider changed |
| `stream_down` / `stream_up` | stream lost (no data / zero bitrate) and back (`down_duration_s`) |
| `tsp_exit` | tsp exited and is restarted (at most once per 10 min per stream, with `exits` count) |
| `scte35` | SCTE-35 commands (`splice_insert`, `time_signal`, …; `splice_null` heartbeats excluded) and events (`event_type=out\|in`, `progress=pending\|occurred`, `pre_roll_ms`) with the raw splicemonitor JSON in `scte` |

```json
{"time":"2026-10-06T09:36:07.1Z","stream":"238.8.7.6:4440","description":"Comedy| …","kind":"scte35","type":"event",
 "event_type":"out","progress":"occurred","event_id":1,"pre_roll_ms":3948,"message":"SCTE-35: начало рекламы (out), id 1, pre-roll 3.9 с","scte":{…}}
```

Alloy ships it to Loki (`deploy/config.alloy`): labels `job="tsmonitor-events"`, `kind`, `stream`.

## 📈 Grafana Dashboards

Generated by `grafana-dashboards/build.py` (`--push` uploads to monitoring.otcnet.ru):

1. **Overview** (`ts-stream-overview`): counters (online / offline / CC errors / ad breaks / host UDP drops),
   status mosaic (green online, orange online with CC errors in 5 min, red offline — click opens the stream),
   problem streams for the last hour, current ad breaks, event log of all streams.
2. **Stream details** (`ts-stream-details?var-stream=…`): status, bitrate (TS / net / per PID), PIDs with codec,
   language and resolution, CC / TEI, IAT and PCR jitter, PSI/SI table intervals (ETR 290 limits),
   SCTE-35 ad breaks and pre-roll, stream event log and SCTE-35 log from Loki, annotations for ad breaks,
   CC error bursts, PID changes and stream down/up.

## 🔧 Development

### Project Structure
```
tsmonitor/
├── cmd/tsmonitor/         # Main application
├── internal/
│   ├── config/            # Configuration
│   ├── tsp/               # tsp command line, runner, output parsers (testdata = real captures)
│   ├── stream/            # Per-stream state built from tsp output
│   ├── metrics/           # Prometheus collector
│   ├── eventlog/          # SCTE-35 JSON-lines log
│   └── monitor/           # Orchestrator
├── grafana-dashboards/
├── deploy/                # systemd unit, sysctl, logrotate
└── config.yaml.example
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
tsp -I ip --local-address 172.22.2.154 233.32.71.45:5000 -P iat -i 5 -P analyze -i 5 --json-line=AN: -O drop
```


## 👥 Authors

- Vladimir Plaksin (@asspye)

## 🙏 Acknowledgments

- TSDuck - The MPEG Transport Stream Toolkit
- Prometheus - Monitoring system & time series database
- Grafana - Analytics & monitoring platform
