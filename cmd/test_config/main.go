package main

import (
	"fmt"
	"os"

	"github.com/otcnet/tsmonitor/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: test_config <config.yaml>")
		fmt.Println("Example: test_config ./config.yaml")
		os.Exit(1)
	}

	configPath := os.Args[1]
	
	fmt.Printf("Loading config from: %s\n\n", configPath)
	
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("❌ Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Config loaded successfully!")
	fmt.Println()
	fmt.Println("=== Configuration ===")
	fmt.Printf("Interface:    %s\n", cfg.Interface)
	fmt.Printf("Metrics Port: %d\n", cfg.MetricsPort)
	fmt.Printf("Timeout:      %v\n", cfg.Timeout)
	fmt.Printf("Streams:      %d\n", cfg.StreamCount())
	fmt.Println()
	
	fmt.Println("=== Streams ===")
	for i, stream := range cfg.Streams {
		fmt.Printf("%3d. %s\n", i+1, stream.URL)
		fmt.Printf("     %s\n", stream.Description)
	}
	
	fmt.Println()
	fmt.Printf("Total: %d streams configured\n", cfg.StreamCount())
}
