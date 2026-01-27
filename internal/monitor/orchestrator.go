package monitor

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/otcnet/tsmonitor/internal/config"
	"github.com/otcnet/tsmonitor/internal/metrics"
	"github.com/otcnet/tsmonitor/internal/tsp"
)

// Orchestrator управляет всеми StreamingRunner'ами и метриками
type Orchestrator struct {
	config   *config.Config
	exporter *metrics.Exporter
	runners  map[string]*tsp.StreamingRunner
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// NewOrchestrator создаёт новый orchestrator
func NewOrchestrator(cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		config:   cfg,
		exporter: metrics.NewExporter(),
		runners:  make(map[string]*tsp.StreamingRunner),
	}
}

// Start запускает мониторинг всех потоков
func (o *Orchestrator) Start(ctx context.Context) error {
	// Регистрируем метрики в Prometheus
	if err := o.exporter.Register(); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	// Запускаем HTTP сервер для метрик
	go o.startMetricsServer()

	// Запускаем runner для каждого потока
	for _, stream := range o.config.Streams {
		if err := o.startStreamMonitoring(ctx, stream); err != nil {
			return fmt.Errorf("failed to start monitoring for %s: %w", stream.URL, err)
		}
	}

	fmt.Printf("✅ Started monitoring %d streams\n", len(o.config.Streams))
	fmt.Printf("📊 Metrics available at http://0.0.0.0:%d/metrics\n", o.config.MetricsPort)

	return nil
}

// startStreamMonitoring запускает мониторинг одного потока
func (o *Orchestrator) startStreamMonitoring(ctx context.Context, stream config.Stream) error {
	runner := tsp.NewStreamingRunner(
		o.config.Interface,
		stream.URL,
		stream.Description,
	)

	// Сохраняем runner
	o.mu.Lock()
	o.runners[stream.URL] = runner
	o.mu.Unlock()

	// Запускаем runner
	if err := runner.Start(ctx); err != nil {
		return err
	}

	// Запускаем горутину для чтения метрик
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.processMetrics(runner)
	}()

	return nil
}

// processMetrics читает метрики из канала и обновляет Prometheus
func (o *Orchestrator) processMetrics(runner *tsp.StreamingRunner) {
	for metrics := range runner.MetricsChan {
		// Обновляем Prometheus метрики
		o.exporter.UpdateMetrics(metrics)
	}
}

// startMetricsServer запускает HTTP сервер для Prometheus метрик
func (o *Orchestrator) startMetricsServer() {
	mux := http.NewServeMux()
	
	// Endpoint для метрик
	mux.Handle("/metrics", promhttp.Handler())
	
	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK\n")
	})
	
	// Информация о статусе
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		runnerCount := len(o.runners)
		o.mu.Unlock()
		
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body>")
		fmt.Fprintf(w, "<h1>TSMonitor</h1>")
		fmt.Fprintf(w, "<p>Monitoring %d streams</p>", runnerCount)
		fmt.Fprintf(w, "<ul>")
		fmt.Fprintf(w, "<li><a href='/metrics'>/metrics</a> - Prometheus metrics</li>")
		fmt.Fprintf(w, "<li><a href='/health'>/health</a> - Health check</li>")
		fmt.Fprintf(w, "</ul>")
		fmt.Fprintf(w, "</body></html>")
	})

	addr := fmt.Sprintf(":%d", o.config.MetricsPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("❌ HTTP server error: %v\n", err)
	}
}

// Stop останавливает все runner'ы
func (o *Orchestrator) Stop() {
	fmt.Println("🛑 Stopping all runners...")
	
	o.mu.Lock()
	for _, runner := range o.runners {
		runner.Stop()
	}
	o.mu.Unlock()

	// Ждём завершения всех горутин
	o.wg.Wait()
	
	fmt.Println("✅ All runners stopped")
}

// GetRunnerCount возвращает количество активных runner'ов
func (o *Orchestrator) GetRunnerCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.runners)
}
