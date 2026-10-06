# Deployment

Requires TSDuck >= 3.40 (`iat` plugin; tested with 3.45-4798) and `tsp` in PATH.

```bash
# Binary (built with: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/tsmonitor ./cmd/tsmonitor)
install -m 755 bin/tsmonitor /home/asspye/tsmonitor/bin/tsmonitor

# Larger UDP receive buffers
sudo cp 99-tsmonitor.conf /etc/sysctl.d/ && sudo sysctl --system

# Event log rotation
sudo cp tsmonitor.logrotate /etc/logrotate.d/tsmonitor

# Service
sudo cp tsmonitor.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now tsmonitor
sudo journalctl -u tsmonitor -f
```

Stream events (SCTE-35, CC error bursts, PID changes, up/down) are written to
`/var/log/tsmonitor/events.log` (`event_log` in config.yaml), one JSON object per line.
`config.alloy` ships it to Loki and host metrics to Prometheus:
install Alloy 1.20.0 (.deb from GitHub releases), copy it to `/etc/alloy/config.alloy`,
`usermod -aG adm alloy`, `systemctl enable --now alloy`.
