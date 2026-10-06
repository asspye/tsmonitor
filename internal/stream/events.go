package stream

import (
	"sort"
	"strings"
	"time"

	"github.com/otcnet/tsmonitor/internal/eventlog"
)

// Виды событий в логе
const (
	KindSCTE35         = "scte35"
	KindCCErrors       = "cc_errors"
	KindPIDsChanged    = "pids_changed"
	KindServiceChanged = "service_changed"
	KindStreamDown     = "stream_down"
	KindStreamUp       = "stream_up"
	KindTSPExit        = "tsp_exit"
	KindStreamFlapping = "stream_flapping"
	KindStreamStable   = "stream_stable"
)

// confirmReports — сколько отчётов подряд новый состав PID/сервисов должен
// продержаться, прежде чем считать его изменением (одиночный сбой не логируем)
const confirmReports = 2

// EventLogger пишет события потока
type EventLogger interface {
	Log(stream, description, kind string, f eventlog.Fields)
}

type event struct {
	kind   string
	fields eventlog.Fields
}

func (s *State) emit(log EventLogger, events []event) {
	if log == nil {
		return
	}
	for _, e := range events {
		if _, ok := e.fields["message"]; !ok {
			e.fields["message"] = message(e.kind, e.fields)
		}
		log.Log(s.URL, s.Description, e.kind, e.fields)
	}
}

// ccBurst — серия интервалов analyze подряд с CC-ошибками
type ccBurst struct {
	active bool
	start  time.Time        // начало первого интервала с ошибками
	byPID  map[string]int64 // PID → ошибок за всю серию
	tei    int64
}

// trackCC обновляет серию CC-ошибок по отчёту; s.mu захвачен
func (s *State) trackCC(errs map[string]int64, tei int64, now time.Time) []event {
	var total int64
	for _, n := range errs {
		total += n
	}
	total += tei
	b := &s.cc
	if total > 0 {
		starting := !b.active
		if starting {
			*b = ccBurst{active: true, start: now.Add(-s.Interval), byPID: map[string]int64{}}
		}
		for p, n := range errs {
			b.byPID[p] += n
		}
		b.tei += tei
		// Пока поток нестабилен, каждый его возврат начинается с разрыва CC —
		// эти серии не логируем (их объясняет stream_flapping), метрики считаются как обычно
		if !starting || s.flap.active {
			return nil
		}
		f := eventlog.Fields{
			"phase":        "start",
			"window_start": b.start.Format(time.RFC3339Nano),
			"pids":         errs,
		}
		if tei > 0 {
			f["transport_errors"] = tei
		}
		return []event{{KindCCErrors, f}}
	}
	if b.active {
		if s.flap.active {
			*b = ccBurst{}
			return nil
		}
		return []event{s.endCCBurst(now, "clean")}
	}
	return nil
}

// endCCBurst закрывает серию CC-ошибок; s.mu захвачен
func (s *State) endCCBurst(now time.Time, reason string) event {
	b := &s.cc
	var total int64
	for _, n := range b.byPID {
		total += n
	}
	f := eventlog.Fields{
		"phase":        "end",
		"reason":       reason,
		"window_start": b.start.Format(time.RFC3339Nano),
		"duration_s":   now.Sub(b.start).Seconds(),
		"pids":         b.byPID,
		"total":        total,
	}
	if b.tei > 0 {
		f["transport_errors"] = b.tei
	}
	*b = ccBurst{}
	return event{KindCCErrors, f}
}

// changeTracker подтверждает изменение набора (PID или сервисов) после confirmReports отчётов
type changeTracker struct {
	committed map[string]string
	pending   map[string]string
	count     int
}

// update возвращает (старый, новый) набор, если изменение подтверждено.
// Исходный набор тоже фиксируется только после confirmReports одинаковых отчётов
// (в первом отчёте после старта tsp бывает неполная информация, например без разрешения видео)
// и не логируется. Пустой набор не рассматривается: пропадание потока пишет stream_down.
func (c *changeTracker) update(cur map[string]string) (old, confirmed map[string]string, ok bool) {
	if len(cur) == 0 {
		return nil, nil, false
	}
	if c.committed != nil && equalMaps(cur, c.committed) {
		c.pending, c.count = nil, 0
		return nil, nil, false
	}
	if c.pending != nil && equalMaps(cur, c.pending) {
		c.count++
	} else {
		c.pending, c.count = cur, 1
	}
	if c.count < confirmReports {
		return nil, nil, false
	}
	old = c.committed
	c.committed, c.pending, c.count = cur, nil, 0
	if old == nil {
		return nil, nil, false
	}
	return old, cur, true
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func pidSignature(p PID) string {
	return strings.Join([]string{p.Type, p.Codec, p.Language, p.Resolution}, "/")
}

func pidFields(pid, sig string) map[string]string {
	parts := strings.SplitN(sig, "/", 4)
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	f := map[string]string{"pid": pid, "type": parts[0], "codec": parts[1]}
	if parts[2] != "" {
		f["language"] = parts[2]
	}
	if parts[3] != "" {
		f["resolution"] = parts[3]
	}
	return f
}

// trackPIDs логирует подтверждённое изменение состава PID; s.mu захвачен
func (s *State) trackPIDs(pids []PID) []event {
	cur := make(map[string]string, len(pids))
	for _, p := range pids {
		cur[p.PID] = pidSignature(p)
	}
	old, now, ok := s.pidChanges.update(cur)
	if !ok {
		return nil
	}
	var added, removed []map[string]string
	var changed []map[string]any
	for _, pid := range sortedKeys(now) {
		if was, ok := old[pid]; !ok {
			added = append(added, pidFields(pid, now[pid]))
		} else if was != now[pid] {
			changed = append(changed, map[string]any{"pid": pid, "from": pidFields(pid, was), "to": pidFields(pid, now[pid])})
		}
	}
	for _, pid := range sortedKeys(old) {
		if _, ok := now[pid]; !ok {
			removed = append(removed, pidFields(pid, old[pid]))
		}
	}
	f := eventlog.Fields{}
	if added != nil {
		f["added"] = added
	}
	if removed != nil {
		f["removed"] = removed
	}
	if changed != nil {
		f["changed"] = changed
	}
	return []event{{KindPIDsChanged, f}}
}

// trackServices логирует подтверждённую смену имени/провайдера сервисов; s.mu захвачен
func (s *State) trackServices(services []Service) []event {
	cur := make(map[string]string, len(services))
	for _, sv := range services {
		cur[sv.Name+" / "+sv.Provider] = sv.Type
	}
	old, now, ok := s.serviceChanges.update(cur)
	if !ok {
		return nil
	}
	return []event{{KindServiceChanged, eventlog.Fields{
		"from": sortedKeys(old),
		"to":   sortedKeys(now),
	}}}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Флаппинг: поток, который пропадает flapDowns раз за flapWindow, логируется одним
// событием stream_flapping вместо пары down/up на каждый цикл; stream_stable —
// когда переходов не было flapStableAfter.
const (
	flapDowns       = 3
	flapWindow      = 10 * time.Minute
	flapStableAfter = 10 * time.Minute
)

// flapState — недавние пропадания и состояние флаппинга
type flapState struct {
	recentDowns    []time.Time // пропадания за последние flapWindow
	active         bool
	since          time.Time // начало флаппинга
	downs          int       // пропаданий с начала флаппинга
	lastTransition time.Time
}

// CheckStatus логирует переходы online ↔ offline. Вызывается периодически.
func (s *State) CheckStatus(now time.Time, log EventLogger) {
	s.mu.Lock()
	online := s.onlineLocked(now)
	fl := &s.flap
	var events []event
	switch {
	case !s.statusKnown:
		if online {
			s.statusKnown, s.wasOnline = true, true
		} else if now.Sub(s.created) >= staleFactor*s.Interval {
			s.statusKnown, s.wasOnline, s.downSince = true, false, now
			events = append(events, event{KindStreamDown, eventlog.Fields{"reason": "no data since monitor start"}})
		}

	case s.wasOnline && !online:
		wasFlapping := fl.active
		s.wasOnline, s.downSince = false, now
		s.downs++
		if fl.active {
			fl.downs++
		}
		fl.lastTransition = now
		fl.recentDowns = append(pruneBefore(fl.recentDowns, now.Add(-flapWindow)), now)
		switch {
		case fl.active:
			// во время флаппинга отдельные пропадания не пишем
		case len(fl.recentDowns) >= flapDowns:
			n := len(fl.recentDowns)
			period := fl.recentDowns[n-1].Sub(fl.recentDowns[0]).Seconds() / float64(n-1)
			*fl = flapState{active: true, since: fl.recentDowns[0], downs: n, recentDowns: fl.recentDowns, lastTransition: now}
			events = append(events, event{KindStreamFlapping, eventlog.Fields{
				"downs":    n,
				"window_s": flapWindow.Seconds(),
				"period_s": period,
			}})
		default:
			f := eventlog.Fields{"reason": "zero bitrate"}
			if s.lastReport.IsZero() || now.Sub(s.lastReport) >= staleFactor*s.Interval {
				f["reason"] = "no data"
			}
			if !s.lastReport.IsZero() {
				f["last_report"] = s.lastReport.Format(time.RFC3339Nano)
			}
			events = append(events, event{KindStreamDown, f})
		}
		if s.cc.active {
			if wasFlapping {
				s.cc = ccBurst{} // серия во время флаппинга не логировалась
			} else {
				events = append(events, s.endCCBurst(now, "stream_down"))
			}
		}

	case !s.wasOnline && online:
		s.wasOnline = true
		fl.lastTransition = now
		if !fl.active {
			events = append(events, event{KindStreamUp, eventlog.Fields{
				"down_since":      s.downSince.Format(time.RFC3339Nano),
				"down_duration_s": now.Sub(s.downSince).Seconds(),
			}})
		}

	case fl.active && now.Sub(fl.lastTransition) >= flapStableAfter:
		state := "offline"
		if s.wasOnline {
			state = "online"
		}
		events = append(events, event{KindStreamStable, eventlog.Fields{
			"state":      state,
			"flapping_s": fl.lastTransition.Sub(fl.since).Seconds(),
			"downs":      fl.downs,
		}})
		*fl = flapState{}
	}
	s.mu.Unlock()
	s.emit(log, events)
}

func pruneBefore(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && ts[i].Before(cutoff) {
		i++
	}
	return append([]time.Time(nil), ts[i:]...)
}

// tspExitLogEvery — tsp_exit пишем не чаще этого на поток (tsp, который не может
// запуститься, перезапускается каждые 5 с)
const tspExitLogEvery = 10 * time.Minute

// TSPExited отмечает завершение tsp (он будет перезапущен)
func (s *State) TSPExited(err error, now time.Time, log EventLogger) {
	s.mu.Lock()
	s.restarts++
	s.exitsSinceLog++
	if !s.lastExitLog.IsZero() && now.Sub(s.lastExitLog) < tspExitLogEvery {
		s.mu.Unlock()
		return
	}
	exits := s.exitsSinceLog
	s.exitsSinceLog, s.lastExitLog = 0, now
	s.mu.Unlock()

	msg := "exited"
	if err != nil {
		msg = err.Error()
	}
	s.emit(log, []event{{KindTSPExit, eventlog.Fields{"error": msg, "exits": exits}}})
}
