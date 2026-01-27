package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/otcnet/tsmonitor/internal/tsp"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	runner := tsp.NewStreamingRunner(
		"172.22.2.154",
		"233.198.134.1:3333",
		"Silk Way Test",
	)

	fmt.Println("Starting streaming runner...")
	if err := runner.Start(ctx); err != nil {
		fmt.Printf("Failed to start: %v\n", err)
		return
	}

	fmt.Println("Runner started. Waiting for metrics...")
	fmt.Println("Press Ctrl+C to stop\n")

	updateCount := 0

	// БЫСТРАЯ горутина для чтения метрик
	go func() {
		for metrics := range runner.MetricsChan {
			updateCount++
			
			// Выводим КРАТКО - одна строка!
			fmt.Printf("[%s] Update #%d: Status=%v, Bitrate=%.2f Mbps, PIDs=%d\n",
				metrics.LastSeen.Format("15:04:05"),
				updateCount,
				metrics.Status,
				float64(metrics.Bitrate.TotalBPS)/1000000,
				len(metrics.PIDs))
		}
	}()

	<-sigChan
	fmt.Println("\nStopping...")
	cancel()
	runner.Stop()
	time.Sleep(1 * time.Second)
	
	fmt.Printf("Total updates: %d\n", updateCount)
}
