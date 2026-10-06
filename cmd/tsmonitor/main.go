package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/otcnet/tsmonitor/internal/config"
	"github.com/otcnet/tsmonitor/internal/monitor"
)

const (
	defaultConfigPath = "/etc/tsmonitor/config.yaml"
	version           = "2.0.0"
)

func main() {
	fmt.Printf("TSMonitor v%s - MPEG-TS Stream Monitor\n\n", version)

	// Определяем путь к конфигу
	configPath := defaultConfigPath
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Загружаем конфигурацию
	fmt.Printf("📝 Loading config from: %s\n", configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		fmt.Println("\nUsage: tsmonitor [config.yaml]")
		fmt.Printf("Default config path: %s\n", defaultConfigPath)
		os.Exit(1)
	}

	fmt.Printf("✅ Config loaded: %d streams\n", cfg.StreamCount())
	fmt.Printf("   Interface: %s\n", cfg.Interface)
	fmt.Printf("   Metrics port: %d\n", cfg.MetricsPort)
	fmt.Printf("   Interval: %s, receive buffer: %d, PCR jitter max: %s, SCTE-35: %v\n",
		cfg.Interval, cfg.ReceiveBuffer, cfg.PCRJitterMax, cfg.SCTE35Enabled())
	if cfg.EventLog != "" {
		fmt.Printf("   Event log: %s\n", cfg.EventLog)
	}
	for _, d := range cfg.Duplicates {
		fmt.Printf("⚠️  Duplicate url skipped: %s (%s)\n", d.URL, d.Description)
	}
	fmt.Println()

	// Создаём контекст с отменой
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Обрабатываем сигналы для graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	// Создаём и запускаем orchestrator
	orch, err := monitor.NewOrchestrator(cfg)
	if err != nil {
		fmt.Printf("❌ Failed to create orchestrator: %v\n", err)
		os.Exit(1)
	}

	if err := orch.Start(ctx); err != nil {
		fmt.Printf("❌ Failed to start orchestrator: %v\n", err)
		os.Exit(1)
	}

	// Ждём сигнала остановки
	sig := <-sigChan
	fmt.Printf("\n📡 Received signal: %v\n", sig)
	fmt.Println("🛑 Shutting down gracefully...")

	// Останавливаем orchestrator
	cancel()
	orch.Stop()

	fmt.Println("👋 Goodbye!")
}
