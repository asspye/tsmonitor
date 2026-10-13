package stream

import (
	"time"

	"github.com/otcnet/tsmonitor/internal/eventlog"
)

// Disable помечает поток выключенным: вывод tsp больше не разбирается,
// пропадание потока не считается и не пишется в лог. log=nil — без события
// (поток выключен ещё до запуска мониторинга).
func (s *State) Disable(comment string, log EventLogger) {
	s.mu.Lock()
	if s.disabled {
		s.mu.Unlock()
		return
	}
	s.disabled = true
	s.statusKnown, s.wasOnline = false, false
	s.cc = ccBurst{}
	s.flap = flapState{}
	s.mu.Unlock()

	f := eventlog.Fields{}
	if comment != "" {
		f["comment"] = comment
	}
	s.emit(log, []event{{KindStreamDisabled, f}})
}

// Enable снова включает поток. Данные до выключения забываются, отсчёт
// «нет данных с запуска» начинается заново.
func (s *State) Enable(now time.Time, log EventLogger) {
	s.mu.Lock()
	if !s.disabled {
		s.mu.Unlock()
		return
	}
	s.disabled = false
	s.created = now
	s.lastReport = time.Time{}
	s.bitrateTotal, s.bitrateNet = 0, 0
	s.pids, s.services = nil, nil
	s.tables = map[string]Table{}
	s.iat = nil
	s.mu.Unlock()

	s.emit(log, []event{{KindStreamEnabled, eventlog.Fields{}}})
}

// Enabled — поток не выключен через API
func (s *State) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.disabled
}

// Online — поток включён и есть свежий отчёт с ненулевым битрейтом
func (s *State) Online(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.disabled && s.onlineLocked(now)
}
