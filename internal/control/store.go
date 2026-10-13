// Package control хранит потоки, выключенные через API, отдельно от config.yaml:
// конфиг остаётся списком всех потоков, а выключение переживает перезапуск сервиса.
package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry — выключенный поток
type Entry struct {
	Since   time.Time `json:"since"`
	Comment string    `json:"comment,omitempty"`
}

type fileData struct {
	Disabled map[string]Entry `json:"disabled"` // url → когда и зачем выключен
}

// Store — набор выключенных потоков, сохраняемый в JSON-файл.
// Пустой путь — только в памяти (для тестов и локального запуска).
type Store struct {
	path string

	mu       sync.Mutex
	disabled map[string]Entry
}

// Open читает файл состояния. Отсутствующий файл — все потоки включены.
func Open(path string) (*Store, error) {
	s := &Store{path: path, disabled: map[string]Entry{}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state file: %w", err)
	}
	var fd fileData
	if err := json.Unmarshal(data, &fd); err != nil {
		return nil, fmt.Errorf("state file %s: %w", path, err)
	}
	for url, e := range fd.Disabled {
		s.disabled[url] = e
	}
	return s, nil
}

// Disabled сообщает, выключен ли поток
func (s *Store) Disabled(url string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.disabled[url]
	return e, ok
}

// SetDisabled выключает поток (или меняет комментарий) и сохраняет файл
func (s *Store) SetDisabled(url string, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.disabled[url]
	s.disabled[url] = e
	if err := s.saveLocked(); err != nil {
		if had {
			s.disabled[url] = old
		} else {
			delete(s.disabled, url)
		}
		return err
	}
	return nil
}

// SetEnabled включает поток и сохраняет файл
func (s *Store) SetEnabled(url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.disabled[url]
	if !had {
		return nil
	}
	delete(s.disabled, url)
	if err := s.saveLocked(); err != nil {
		s.disabled[url] = old
		return err
	}
	return nil
}

// saveLocked пишет файл через временный и rename, чтобы сбой не оставил его обрезанным
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(fileData{Disabled: s.disabled}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}
