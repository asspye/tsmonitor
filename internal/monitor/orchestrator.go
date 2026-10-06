package monitor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/otcnet/tsmonitor/internal/config"
	"github.com/otcnet/tsmonitor/internal/eventlog"
	"github.com/otcnet/tsmonitor/internal/metrics"
	"github.com/otcnet/tsmonitor/internal/stream"
	"github.com/otcnet/tsmonitor/internal/tsp"
)

// startStagger — пауза между запусками tsp, чтобы не стартовать сотни процессов разом
const startStagger = 20 * time.Millisecond

// Orchestrator запускает tsp для каждого потока и отдаёт метрики
type Orchestrator struct {
	config  *config.Config
	states  []*stream.State
	sources []metrics.Source
	events  *eventlog.Logger
	server  *http.Server
	wg      sync.WaitGroup
}

// NewOrchestrator создаёт orchestrator
func NewOrchestrator(cfg *config.Config) (*Orchestrator, error) {
	events, err := eventlog.Open(cfg.EventLog)
	if err != nil {
		return nil, err
	}
	o := &Orchestrator{config: cfg, events: events}
	for _, s := range cfg.Streams {
		st := stream.New(s.URL, s.Description, cfg.Interval)
		o.states = append(o.states, st)
		o.sources = append(o.sources, metrics.Source{
			State:        st,
			PCRMonitored: cfg.PCRJitterMax > 0 && !tsp.IsHLS(s.URL),
		})
	}
	return o, nil
}

// Start регистрирует метрики, поднимает HTTP и запускает tsp по всем потокам
func (o *Orchestrator) Start(ctx context.Context) error {
	if err := prometheus.Register(metrics.NewCollector(o.sources)); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	if err := o.startMetricsServer(); err != nil {
		return err
	}

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.watchStatus(ctx)
	}()

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		for _, st := range o.states {
			o.startStream(ctx, st)
			select {
			case <-ctx.Done():
				return
			case <-time.After(startStagger):
			}
		}
		fmt.Printf("✅ Started monitoring %d streams\n", len(o.states))
	}()

	fmt.Printf("📊 Metrics available at http://0.0.0.0:%d/metrics\n", o.config.MetricsPort)
	return nil
}

// watchStatus раз в секунду проверяет переходы online ↔ offline для лога событий
func (o *Orchestrator) watchStatus(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, st := range o.states {
				st.CheckStatus(now, o.events)
			}
		}
	}
}

func (o *Orchestrator) startStream(ctx context.Context, st *stream.State) {
	r := &tsp.Runner{
		Opts: tsp.Options{
			LocalAddress:  o.config.Interface,
			URL:           st.URL,
			Interval:      o.config.Interval,
			ReceiveBuffer: o.config.ReceiveBuffer,
			PCRJitterMax:  o.config.PCRJitterMax,
			SCTE35:        o.config.SCTE35Enabled(),
		},
		OnLine: func(line string) {
			st.HandleLine(line, time.Now(), o.events)
		},
		OnExit: func(err error) {
			st.TSPExited(err, time.Now(), o.events)
			fmt.Printf("[%s] %v, restarting\n", st.URL, err)
		},
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		r.Run(ctx)
	}()
}

func (o *Orchestrator) startMetricsServer() error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK\n")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body><h1>TSMonitor</h1><p>Monitoring %d streams</p>", len(o.states))
		fmt.Fprintf(w, "<ul><li><a href='/metrics'>/metrics</a> - Prometheus metrics</li>")
		fmt.Fprintf(w, "<li><a href='/health'>/health</a> - Health check</li></ul></body></html>")
	})

	// Слушаем порт сразу: если он занят, сервис должен упасть, а не работать без /metrics
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", o.config.MetricsPort))
	if err != nil {
		return fmt.Errorf("metrics port: %w", err)
	}
	o.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := o.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Printf("❌ HTTP server error: %v\n", err)
		}
	}()
	return nil
}

// Stop ждёт остановки всех tsp (ctx уже отменён вызывающим) и закрывает ресурсы
func (o *Orchestrator) Stop() {
	fmt.Println("🛑 Stopping all runners...")
	o.wg.Wait()
	if o.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		o.server.Shutdown(ctx)
		cancel()
	}
	o.events.Close()
	fmt.Println("✅ All runners stopped")
}
