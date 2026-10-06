// Package eventlog пишет события потоков по одной JSON-строке
// (SCTE-35, начало/конец CC-ошибок, смена PID, пропадание потока…) —
// для отправки в Loki (Alloy читает файл) и показа в Grafana.
package eventlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/otcnet/tsmonitor/internal/tsp"
)

// Fields — поля события помимо общих (time, stream, description, kind)
type Fields map[string]any

// Logger — потокобезопасный писатель JSON-строк
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	prefix string
	closer io.Closer
	now    func() time.Time
}

// Open открывает файл на дозапись. Пустой путь — писать в stdout с префиксом "EVENT ".
// Файл открыт с O_APPEND, поэтому logrotate с copytruncate работает без сигналов.
func Open(path string) (*Logger, error) {
	if path == "" {
		return &Logger{w: os.Stdout, prefix: "EVENT ", now: time.Now}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("event log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("event log: %w", err)
	}
	return &Logger{w: f, closer: f, now: time.Now}, nil
}

// New — логгер в произвольный writer (для тестов)
func New(w io.Writer) *Logger {
	return &Logger{w: w, now: time.Now}
}

// Close закрывает файл
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// Log пишет одно событие потока
func (l *Logger) Log(streamURL, description, kind string, f Fields) {
	rec := make(map[string]any, len(f)+4)
	for k, v := range f {
		rec[k] = v
	}
	rec["time"] = l.now().Format(time.RFC3339Nano)
	rec["stream"] = streamURL
	rec["description"] = description
	rec["kind"] = kind

	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s%s\n", l.prefix, line)
}

// SCTEFields — поля события kind=scte35. Поля верхнего уровня — для фильтров в Loki,
// scte — исходный JSON splicemonitor целиком.
func SCTEFields(m *tsp.SCTE35) Fields {
	f := Fields{"scte": m.Raw}
	switch {
	case m.IsEvent && m.Event != nil:
		e := m.Event
		f["type"] = "event"
		f["event_type"] = e.Type // out / in
		f["progress"] = e.Progress
		f["count"] = e.Count
		f["splice_pid"] = e.SplicePID
		f["event_id"] = e.EventID
		if !e.Occurred() {
			f["time_to_event_ms"] = e.TimeToEventMS
		}
		if e.PreRollMS != nil {
			f["pre_roll_ms"] = *e.PreRollMS
		}
	default:
		f["type"] = "command"
		f["command"] = m.Command
		f["splice_pid"] = m.PID
		if len(m.Descriptors) > 0 {
			f["descriptors"] = m.Descriptors
		}
		if ins := m.Insert; ins != nil {
			f["event_id"] = ins.EventID
			f["out_of_network"] = ins.OutOfNetwork
			if ins.BreakDuration > 0 {
				f["break_duration_s"] = ins.BreakDuration
			}
			if ins.Cancel {
				f["cancel"] = true
			}
		}
	}
	return f
}
