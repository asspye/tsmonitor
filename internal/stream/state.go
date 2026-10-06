// Package stream хранит состояние одного потока, собранное из вывода tsp.
// Метрики строятся из снимка состояния в момент опроса Prometheus,
// поэтому серии исчезнувших PID/сервисов пропадают сами.
package stream

import (
	"fmt"
	"sync"
	"time"

	"github.com/otcnet/tsmonitor/internal/eventlog"
	"github.com/otcnet/tsmonitor/internal/tsp"
)

// PID — элементарный поток из последнего отчёта analyze
type PID struct {
	PID        string // "0x0100"
	Type       string // video / audio / data / other
	Codec      string
	Language   string
	Resolution string
	BitrateBPS int64
}

// Service — сервис из последнего отчёта analyze
type Service struct {
	Name     string
	Provider string
	Type     string // HD / SD
}

// Table — интервалы повторения таблицы за последний интервал analyze
type Table struct {
	AvgSeconds float64
	MaxSeconds float64
}

// pidForget — через сколько забываем счётчик CC у PID, пропавшего из потока
const pidForget = time.Hour

// State — состояние потока. Все поля защищены mu.
type State struct {
	URL         string
	Description string
	Interval    time.Duration

	mu sync.Mutex

	lastReport   time.Time
	bitrateTotal int64
	bitrateNet   int64
	pids         []PID
	services     []Service
	tables       map[string]Table

	ccErrors        map[string]float64   // PID → накопленные разрывы CC
	ccSeen          map[string]time.Time // PID → когда последний раз был в отчёте
	resolution      map[string]string    // PID → последнее известное разрешение видео
	transportErrors float64
	syncErrors      float64

	iat     *tsp.IATReport
	iatTime time.Time

	pcrExceeded   float64
	pcrMaxCurrent int64 // мкс, максимум в текущем интервале
	pcrMaxLast    int64 // мкс, максимум за последний завершённый интервал

	restarts float64
	parseErr float64

	scte scteState

	// Для лога событий
	created        time.Time
	cc             ccBurst
	pidChanges     changeTracker
	serviceChanges changeTracker
	statusKnown    bool
	wasOnline      bool
	downSince      time.Time
	lastExitLog    time.Time
	exitsSinceLog  int
	downs          float64 // переходов online → offline
	flap           flapState
}

type scteState struct {
	commands    map[string]float64
	lastCommand time.Time
	events      map[string]float64   // направление → счётчик наступивших событий
	lastEvent   map[string]time.Time // направление → время последнего события
	preroll     map[string]float64   // направление → pre-roll первого анонса, с

	announcedDuration float64 // break_duration последнего splice_insert out
	breakDuration     float64 // длительность текущего/последнего блока
}

// New создаёт состояние потока
func New(url, description string, interval time.Duration) *State {
	return &State{
		URL:         url,
		Description: description,
		Interval:    interval,
		created:     time.Now(),
		tables:      map[string]Table{},
		ccErrors:    map[string]float64{},
		ccSeen:      map[string]time.Time{},
		resolution:  map[string]string{},
		scte: scteState{
			commands:  map[string]float64{},
			events:    map[string]float64{},
			lastEvent: map[string]time.Time{},
			preroll:   map[string]float64{},
		},
	}
}

// HandleLine разбирает одну строку вывода tsp
func (s *State) HandleLine(line string, now time.Time, log EventLogger) {
	kind, payload := tsp.Classify(line)
	switch kind {
	case tsp.LineAnalyze:
		r, err := tsp.ParseAnalyze(payload)
		if err != nil {
			s.countParseError()
			return
		}
		s.emit(log, s.applyAnalyze(r, now))
	case tsp.LineIAT:
		r, err := tsp.ParseIAT(payload)
		if err != nil {
			s.countParseError()
			return
		}
		s.mu.Lock()
		s.iat, s.iatTime = r, now
		s.mu.Unlock()
	case tsp.LinePCRJitter:
		j, err := tsp.ParsePCRJitter(payload)
		if err != nil {
			s.countParseError()
			return
		}
		s.mu.Lock()
		s.pcrExceeded++
		if j.JitterUS > s.pcrMaxCurrent {
			s.pcrMaxCurrent = j.JitterUS
		}
		s.mu.Unlock()
	case tsp.LineSCTE:
		m, err := tsp.ParseSCTE(payload)
		if err != nil {
			s.countParseError()
			return
		}
		s.applySCTE(m, now)
		if !(m.IsTable && m.Command == "splice_null") {
			s.emit(log, []event{{KindSCTE35, eventlog.SCTEFields(m)}})
		}
	}
}

func (s *State) countParseError() {
	s.mu.Lock()
	s.parseErr++
	s.mu.Unlock()
}

// applyAnalyze применяет отчёт и возвращает события для лога
func (s *State) applyAnalyze(r *tsp.AnalyzeReport, now time.Time) []event {
	var stuffing int64
	var pids []PID
	for i := range r.PIDs {
		p := &r.PIDs[i]
		if p.ID == tsp.StuffingPID {
			stuffing = p.Bitrate
			continue
		}
		if !p.IsElementary() {
			continue
		}
		pids = append(pids, PID{
			PID:        pidHex(p.ID),
			Type:       tsp.PIDType(p),
			Codec:      tsp.Codec(p.Description),
			Language:   p.Language,
			Resolution: tsp.Resolution(p.Description),
			BitrateBPS: p.Bitrate,
		})
	}

	var services []Service
	for _, sv := range r.Services {
		if sv.Name == "" || sv.Name == "(unknown)" {
			continue
		}
		services = append(services, Service{
			Name:     sv.Name,
			Provider: tsp.CleanProvider(sv.Provider),
			Type:     tsp.ServiceType(sv.TypeName),
		})
	}

	tables := map[string]Table{}
	for _, t := range r.Tables {
		name, ok := tsp.TableNames[t.TID]
		if !ok || t.RepetitionMS <= 0 {
			continue
		}
		cur := Table{AvgSeconds: float64(t.RepetitionMS) / 1000, MaxSeconds: float64(t.MaxRepetitionMS) / 1000}
		// PMT несколько (по одной на сервис) — берём худшую
		if prev, ok := tables[name]; ok {
			if prev.AvgSeconds > cur.AvgSeconds {
				cur.AvgSeconds = prev.AvgSeconds
			}
			if prev.MaxSeconds > cur.MaxSeconds {
				cur.MaxSeconds = prev.MaxSeconds
			}
		}
		tables[name] = cur
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Разрешение видео analyze находит, только если за интервал пришёл SPS / sequence header,
	// поэтому в части отчётов его нет — держим последнее известное
	for i := range pids {
		p := &pids[i]
		if p.Resolution != "" {
			s.resolution[p.PID] = p.Resolution
		} else {
			p.Resolution = s.resolution[p.PID]
		}
	}
	s.lastReport = now
	s.bitrateTotal = r.TS.Bitrate
	s.bitrateNet = r.TS.Bitrate - stuffing
	s.pids = pids
	s.services = services
	s.tables = tables
	s.transportErrors += float64(r.TS.Packets.TransportErrors)
	s.syncErrors += float64(r.TS.Packets.InvalidSyncs)
	ccNow := map[string]int64{}
	for i := range r.PIDs {
		p := &r.PIDs[i]
		if p.ID == tsp.StuffingPID {
			continue // у нулевых пакетов CC не проверяется
		}
		// += 0 тоже создаёт ключ: серия есть и у PID без ошибок
		key := pidHex(p.ID)
		s.ccErrors[key] += float64(p.Packets.Discontinuities)
		s.ccSeen[key] = now
		if p.Packets.Discontinuities > 0 {
			ccNow[key] = p.Packets.Discontinuities
		}
	}
	for key, seen := range s.ccSeen {
		if now.Sub(seen) > pidForget {
			delete(s.ccErrors, key)
			delete(s.ccSeen, key)
			delete(s.resolution, key)
		}
	}
	s.pcrMaxLast, s.pcrMaxCurrent = s.pcrMaxCurrent, 0

	var events []event
	events = append(events, s.trackCC(ccNow, r.TS.Packets.TransportErrors, now)...)
	if r.TS.Bitrate > 0 {
		events = append(events, s.trackPIDs(pids)...)
		events = append(events, s.trackServices(services)...)
	}
	return events
}

// onlineLocked — есть свежий отчёт analyze с ненулевым битрейтом; s.mu захвачен
func (s *State) onlineLocked(now time.Time) bool {
	return !s.lastReport.IsZero() &&
		now.Sub(s.lastReport) < staleFactor*s.Interval &&
		s.bitrateTotal > 0
}

func (s *State) applySCTE(m *tsp.SCTE35, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := &s.scte
	if m.IsTable {
		sc.commands[m.Command]++
		sc.lastCommand = now
		if m.Insert != nil && m.Insert.OutOfNetwork && !m.Insert.Cancel {
			sc.announcedDuration = m.Insert.BreakDuration
		}
		return
	}
	e := m.Event
	if e == nil || (e.Type != "out" && e.Type != "in") {
		return
	}
	if !e.Occurred() {
		if e.Count == 1 {
			sc.preroll[e.Type] = float64(e.TimeToEventMS) / 1000
		}
		return
	}
	sc.events[e.Type]++
	sc.lastEvent[e.Type] = now
	if e.PreRollMS != nil {
		sc.preroll[e.Type] = float64(*e.PreRollMS) / 1000
	}
	if e.Type == "out" {
		sc.breakDuration = sc.announcedDuration
	}
}

func pidHex(id int) string {
	return fmt.Sprintf("0x%04X", id)
}
