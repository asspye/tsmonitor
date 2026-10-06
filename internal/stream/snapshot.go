package stream

import (
	"time"

	"github.com/otcnet/tsmonitor/internal/tsp"
)

// staleFactor — поток offline, если отчёта analyze нет дольше staleFactor×Interval
const staleFactor = 3

// maxBreakWithoutDuration — сколько считаем блок активным, если в splice_insert
// не было break_duration и не пришёл возврат (in)
const maxBreakWithoutDuration = time.Hour

// breakGrace — запас к заявленной длительности блока, прежде чем считать его законченным
const breakGrace = 30 * time.Second

// Snapshot — копия состояния потока на момент опроса
type Snapshot struct {
	URL         string
	Description string
	Online      bool

	BitrateTotal int64
	BitrateNet   int64
	PIDs         []PID
	Services     []Service
	Tables       map[string]Table
	LastReport   time.Time

	CCErrors        map[string]float64
	TransportErrors float64
	SyncErrors      float64

	IAT *tsp.IATReport // nil, если данных нет или они устарели

	PCRExceeded  float64
	PCRMaxUS     int64
	PCRMonitored bool

	Restarts    float64
	ParseErrors float64
	Downs       float64 // переходов online → offline
	Flapping    bool

	SCTE SCTESnapshot
}

// SCTESnapshot — состояние SCTE-35. Seen=false, если SCTE-35 в потоке не встречался.
type SCTESnapshot struct {
	Seen          bool
	Commands      map[string]float64
	LastCommand   time.Time
	Events        map[string]float64
	LastEvent     map[string]time.Time
	Preroll       map[string]float64
	BreakActive   bool
	BreakDuration float64
}

// Snapshot возвращает копию состояния
func (s *State) Snapshot(now time.Time, pcrMonitored bool) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	stale := staleFactor * s.Interval

	snap := Snapshot{
		URL:             s.URL,
		Description:     s.Description,
		Online:          s.onlineLocked(now),
		LastReport:      s.lastReport,
		CCErrors:        copyMap(s.ccErrors),
		TransportErrors: s.transportErrors,
		SyncErrors:      s.syncErrors,
		PCRExceeded:     s.pcrExceeded,
		PCRMaxUS:        s.pcrMaxLast,
		PCRMonitored:    pcrMonitored,
		Restarts:        s.restarts,
		Downs:           s.downs,
		Flapping:        s.flap.active,
		ParseErrors:     s.parseErr,
		Services:        append([]Service(nil), s.services...),
	}
	if snap.Online {
		snap.BitrateTotal = s.bitrateTotal
		snap.BitrateNet = s.bitrateNet
		snap.PIDs = append([]PID(nil), s.pids...)
		snap.Tables = make(map[string]Table, len(s.tables))
		for k, v := range s.tables {
			snap.Tables[k] = v
		}
	}
	if s.iat != nil && now.Sub(s.iatTime) < stale {
		iat := *s.iat
		snap.IAT = &iat
	}

	sc := &s.scte
	snap.SCTE = SCTESnapshot{
		Seen:          !sc.lastCommand.IsZero() || len(sc.events) > 0,
		Commands:      copyMap(sc.commands),
		LastCommand:   sc.lastCommand,
		Events:        copyMap(sc.events),
		LastEvent:     map[string]time.Time{},
		Preroll:       copyMap(sc.preroll),
		BreakDuration: sc.breakDuration,
	}
	for k, v := range sc.lastEvent {
		snap.SCTE.LastEvent[k] = v
	}
	out, in := sc.lastEvent["out"], sc.lastEvent["in"]
	if !out.IsZero() && out.After(in) {
		limit := maxBreakWithoutDuration
		if sc.breakDuration > 0 {
			limit = time.Duration(sc.breakDuration*float64(time.Second)) + breakGrace
		}
		snap.SCTE.BreakActive = now.Sub(out) < limit
	}
	return snap
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	c := make(map[K]V, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}
