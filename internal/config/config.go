package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config содержит всю конфигурацию приложения.
// Все поля, кроме interface, metrics_port и streams, необязательные —
// старые конфиги загружаются без изменений.
type Config struct {
	Interface   string        `yaml:"interface"`    // IP адрес интерфейса для multicast
	MetricsPort int           `yaml:"metrics_port"` // Порт для Prometheus metrics
	Timeout     time.Duration `yaml:"timeout"`      // Не используется, оставлен для совместимости со старыми конфигами

	// Interval — период отчётов analyze и iat. Поток считается offline,
	// если от tsp нет отчёта дольше 3×Interval.
	Interval time.Duration `yaml:"interval"`

	// ReceiveBuffer — размер буфера UDP-сокета tsp (--buffer-size), байт.
	// 0 — системный размер по умолчанию. Ядро ограничивает его net.core.rmem_max.
	ReceiveBuffer int `yaml:"receive_buffer"`

	// PCRJitterMax — порог pcrverify: PCR с джиттером больше порога
	// считаются нарушениями. 0 — pcrverify не запускается.
	PCRJitterMax time.Duration `yaml:"pcr_jitter_max"`

	// SCTE35 — запускать splicemonitor (по умолчанию включён).
	SCTE35 *bool `yaml:"scte35"`

	// EventLog — файл событий потоков (JSON-строки): SCTE-35, CC-ошибки,
	// смена PID/сервисов, пропадание потока. Пусто — пишем в stdout.
	EventLog string `yaml:"event_log"`

	Streams []Stream `yaml:"streams"` // Список потоков для мониторинга

	// Duplicates — потоки с повторным url, пропущенные при валидации
	Duplicates []Stream `yaml:"-"`
}

// Stream описывает один MPEG-TS поток
type Stream struct {
	URL         string `yaml:"url"`         // Multicast адрес (например: 233.198.134.1:3333) или HLS-плейлист
	Description string `yaml:"description"` // Описание потока
}

const (
	DefaultInterval      = 5 * time.Second
	DefaultReceiveBuffer = 8 * 1024 * 1024
	DefaultPCRJitterMax  = 5 * time.Millisecond
)

// Load загружает конфигурацию из YAML файла
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	cfg := Config{
		Interval:      DefaultInterval,
		ReceiveBuffer: DefaultReceiveBuffer,
		PCRJitterMax:  DefaultPCRJitterMax,
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

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

	if c.Interval == 0 {
		c.Interval = DefaultInterval
	}
	if c.Interval < time.Second {
		return fmt.Errorf("invalid interval: %s (must be >= 1s)", c.Interval)
	}

	if c.ReceiveBuffer < 0 {
		return fmt.Errorf("invalid receive_buffer: %d", c.ReceiveBuffer)
	}

	if c.PCRJitterMax < 0 {
		return fmt.Errorf("invalid pcr_jitter_max: %s", c.PCRJitterMax)
	}

	if len(c.Streams) == 0 {
		return fmt.Errorf("no streams configured")
	}

	// Повторный url пропускаем (метрики двух потоков с одним url смешались бы)
	seen := make(map[string]bool, len(c.Streams))
	unique := c.Streams[:0]
	for i, stream := range c.Streams {
		if stream.URL == "" {
			return fmt.Errorf("stream %d: url is required", i)
		}
		if stream.Description == "" {
			return fmt.Errorf("stream %d: description is required", i)
		}
		if seen[stream.URL] {
			c.Duplicates = append(c.Duplicates, stream)
			continue
		}
		seen[stream.URL] = true
		unique = append(unique, stream)
	}
	c.Streams = unique

	return nil
}

// SCTE35Enabled сообщает, нужно ли запускать splicemonitor
func (c *Config) SCTE35Enabled() bool {
	return c.SCTE35 == nil || *c.SCTE35
}

// StreamCount возвращает количество потоков
func (c *Config) StreamCount() int {
	return len(c.Streams)
}
