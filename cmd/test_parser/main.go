package main

import (
	"fmt"
	"log"
	"time"

	"github.com/otcnet/tsmonitor/internal/tsp"
)

func main() {
	// Создаем runner
	runner := tsp.NewRunner("172.22.2.154", 10*time.Second)

	// Тестируем на реальном потоке
	streamURL := "233.198.134.1:3333"
	description := "Silk Way Test"

	fmt.Printf("Testing stream: %s\n", streamURL)
	fmt.Println("Running tsp command...")

	metrics, err := runner.RunSimple(streamURL, description)
	if err != nil {
		log.Fatalf("Failed to get metrics: %v", err)
	}

	// Выводим результаты
	fmt.Println("\n=== Stream Metrics ===")
	fmt.Printf("URL: %s\n", metrics.StreamURL)
	fmt.Printf("Description: %s\n", metrics.Description)
	fmt.Printf("Status: %v\n", metrics.Status)
	fmt.Printf("Last Seen: %s\n", metrics.LastSeen.Format("2006-01-02 15:04:05"))

	fmt.Println("\n=== Bitrate ===")
	fmt.Printf("Total: %d bps (%.2f Mbps)\n", metrics.Bitrate.TotalBPS, float64(metrics.Bitrate.TotalBPS)/1000000)
	fmt.Printf("Net:   %d bps (%.2f Mbps)\n", metrics.Bitrate.NetBPS, float64(metrics.Bitrate.NetBPS)/1000000)

	fmt.Println("\n=== Service Info ===")
	fmt.Printf("Service Name: %s\n", metrics.ServiceInfo.ServiceName)
	fmt.Printf("Provider: %s\n", metrics.ServiceInfo.Provider)
	fmt.Printf("Type: %s\n", metrics.ServiceInfo.ServiceType)
	fmt.Printf("TS ID: %s\n", metrics.TSID)

	fmt.Println("\n=== PIDs ===")
	for i, pid := range metrics.PIDs {
		fmt.Printf("%d. PID %s (%d) - Type: %s, Codec: %s", 
			i+1, pid.PID, pid.PIDDecimal, pid.Type, pid.Codec)
		if pid.Language != "" {
			fmt.Printf(", Lang: %s", pid.Language)
		}
		if pid.IsSubtitle {
			fmt.Printf(", Subtitle: %s", pid.SubtitleType)
		}
		fmt.Println()
	}
}
