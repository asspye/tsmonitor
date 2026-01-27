package tsp

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StreamingRunner запускает долгоживущий процесс tsp
type StreamingRunner struct {
	LocalInterface string
	StreamURL      string
	Description    string
	
	cmd           *exec.Cmd
	mu            sync.Mutex
	running       bool
	restartDelay  time.Duration
	
	MetricsChan chan *StreamMetrics
}

// NewStreamingRunner создает новый StreamingRunner
func NewStreamingRunner(localInterface, streamURL, description string) *StreamingRunner {
	return &StreamingRunner{
		LocalInterface: localInterface,
		StreamURL:      streamURL,
		Description:    description,
		restartDelay:   5 * time.Second,
		MetricsChan:    make(chan *StreamMetrics, 100), // Увеличили буфер
	}
}

// Start запускает долгоживущий процесс tsp
func (r *StreamingRunner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("runner already running for %s", r.StreamURL)
	}
	r.running = true
	r.mu.Unlock()

	go r.runLoop(ctx)
	return nil
}

// runLoop основной цикл работы
func (r *StreamingRunner) runLoop(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		close(r.MetricsChan)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := r.runTSP(ctx); err != nil {
				fmt.Printf("[%s] tsp error: %v\n", r.StreamURL, err)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(r.restartDelay):
			}
		}
	}
}

// runTSP запускает процесс tsp и читает его вывод
func (r *StreamingRunner) runTSP(ctx context.Context) error {
	args := []string{
		"-I", "ip",
		"--local-address", r.LocalInterface,
		r.StreamURL,
		"-O", "drop",
		"-P", "continuity",
		"-P", "tables", "--all-sections",
		"-P", "bitrate_monitor",
		"-p", "1",
		"-t", "1",
	}

	cmd := exec.CommandContext(ctx, "tsp", args...)
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tsp: %w", err)
	}

	r.mu.Lock()
	r.cmd = cmd
	r.mu.Unlock()

	// Канал для объединения строк из stdout и stderr
	linesChan := make(chan string, 100)
	
	// Читаем stdout в отдельной горутине
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			linesChan <- scanner.Text()
		}
	}()
	
	// Читаем stderr в отдельной горутине  
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			linesChan <- scanner.Text()
		}
	}()

	// Обрабатываем строки
	buffer := strings.Builder{}
	lastUpdate := time.Now()
	const maxBufferSize = 500 * 1024

	// Горутина для проверки таймаута
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Since(lastUpdate) > 10*time.Second {
					offlineMetrics := &StreamMetrics{
						StreamURL:   r.StreamURL,
						Description: r.Description,
						Status:      false,
						LastSeen:    lastUpdate,
						PIDs:        []PIDInfo{},
						CCErrors:    make(map[string]int64),
					}
					
					select {
					case r.MetricsChan <- offlineMetrics:
						lastUpdate = time.Now()
					default:
					}
				}
			}
		}
	}()

	// Основной цикл чтения
	for {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			return nil
			
		case line := <-linesChan:
			buffer.WriteString(line)
			buffer.WriteString("\n")

			// Парсим когда видим bitrate_monitor
			if strings.Contains(line, "bitrate_monitor:") {
				metrics, err := ParseOutput(buffer.String(), r.StreamURL, r.Description)
				if err == nil {
					select {
					case r.MetricsChan <- metrics:
						lastUpdate = time.Now()
					default:
					}
				}

				// Обрезаем буфер если слишком большой
				if buffer.Len() > maxBufferSize {
					content := buffer.String()
					keepFrom := len(content) - maxBufferSize/2
					if keepFrom < 0 {
						keepFrom = 0
					}
					buffer.Reset()
					buffer.WriteString(content[keepFrom:])
				}
			}

			// Защита от переполнения
			if buffer.Len() > maxBufferSize*2 {
				content := buffer.String()
				keepFrom := len(content) - maxBufferSize
				if keepFrom < 0 {
					keepFrom = 0
				}
				buffer.Reset()
				buffer.WriteString(content[keepFrom:])
			}
		}
	}
}

// Stop останавливает процесс tsp
func (r *StreamingRunner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cmd != nil && r.cmd.Process != nil {
		if err := r.cmd.Process.Signal(os.Interrupt); err != nil {
			return r.cmd.Process.Kill()
		}
	}

	return nil
}

// IsRunning проверяет работает ли runner
func (r *StreamingRunner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
