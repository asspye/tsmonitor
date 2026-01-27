# Deployment

## Systemd Service Installation
```bash
# Copy service file
sudo cp tsmonitor.service /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Enable service (autostart on boot)
sudo systemctl enable tsmonitor

# Start service
sudo systemctl start tsmonitor

# Check status
sudo systemctl status tsmonitor

# View logs
sudo journalctl -u tsmonitor -f
```

## Manual Start
```bash
cd /home/asspye/tsmonitor
./bin/tsmonitor config.yaml
```
