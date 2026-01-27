package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config содержит всю конфигурацию приложения
type Config struct {
	Interface   string        `yaml:"interface"`   // IP адрес интерфейса для multicast
	MetricsPort int           `yaml:"metrics_port"` // Порт для Prometheus metrics
	Timeout     time.Duration `yaml:"timeout"`      // Таймаут для команд tsp
	Streams     []Stream      `yaml:"streams"`      // Список потоков для мониторинга
}

// Stream описывает один MPEG-TS поток
type Stream struct {
	URL         string `yaml:"url"`         // Multicast адрес (например: 233.198.134.1:3333)
	Description string `yaml:"description"` // Описание потока
}

// Load загружает конфигурацию из YAML файла
func Load(path string) (*Config, error) {
	// Читаем файл
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Парсим YAML
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Валидация
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate проверяет корректность конфигурации
func (c *Config) Validate() error {
	if c.Interface == "" {
		return fmt.Errorf("interface is required")
	}

	if c.MetricsPort <= 0 || c.MetricsPort > 65535 {
		return fmt.Errorf("invalid metrics_port: %d (must be 1-65535)", c.MetricsPort)
	}

	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second // default
	}

	if len(c.Streams) == 0 {
		return fmt.Errorf("no streams configured")
	}

	// Проверяем каждый поток
	for i, stream := range c.Streams {
		if stream.URL == "" {
			return fmt.Errorf("stream %d: url is required", i)
		}
		if stream.Description == "" {
			return fmt.Errorf("stream %d: description is required", i)
		}
	}

	return nil
}

// StreamCount возвращает количество потоков
func (c *Config) StreamCount() int {
	return len(c.Streams)
}
