package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Создаём временный конфиг
	content := `
interface: "172.22.2.154"
metrics_port: 9090
timeout: 10s

streams:
  - url: "233.198.134.1:3333"
    description: "Test Stream 1"
  
  - url: "233.198.134.91:3333"
    description: "Test Stream 2"
`
	
	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	
	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	// Загружаем конфиг
	cfg, err := Load(tmpfile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Проверяем значения
	if cfg.Interface != "172.22.2.154" {
		t.Errorf("Interface = %s, want 172.22.2.154", cfg.Interface)
	}

	if cfg.MetricsPort != 9090 {
		t.Errorf("MetricsPort = %d, want 9090", cfg.MetricsPort)
	}

	if cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", cfg.Timeout)
	}

	if len(cfg.Streams) != 2 {
		t.Errorf("Streams count = %d, want 2", len(cfg.Streams))
	}

	if cfg.Streams[0].URL != "233.198.134.1:3333" {
		t.Errorf("Stream[0].URL = %s, want 233.198.134.1:3333", cfg.Streams[0].URL)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Interface:   "172.22.2.154",
				MetricsPort: 9090,
				Timeout:     10 * time.Second,
				Streams: []Stream{
					{URL: "233.198.134.1:3333", Description: "Test"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing interface",
			config: Config{
				MetricsPort: 9090,
				Streams: []Stream{
					{URL: "233.198.134.1:3333", Description: "Test"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: Config{
				Interface:   "172.22.2.154",
				MetricsPort: 99999,
				Streams: []Stream{
					{URL: "233.198.134.1:3333", Description: "Test"},
				},
			},
			wantErr: true,
		},
		{
			name: "no streams",
			config: Config{
				Interface:   "172.22.2.154",
				MetricsPort: 9090,
				Streams:     []Stream{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
