package tsp

import (
	"encoding/json"
	"fmt"
)

// SCTE35 — одно сообщение splicemonitor (--json-line):
// либо разобранная таблица splice_information_table, либо событие (event).
type SCTE35 struct {
	Raw json.RawMessage // исходный JSON, уходит в лог событий как есть

	// Таблица
	IsTable     bool
	PID         int
	Command     string   // splice_null, splice_insert, time_signal, …
	Descriptors []string // имена дескрипторов (segmentation_descriptor, …)
	Insert      *SpliceInsert

	// Событие
	IsEvent bool
	Event   *SpliceEvent
}

// SpliceInsert — поля команды splice_insert
type SpliceInsert struct {
	EventID       int64
	OutOfNetwork  bool // true — уход в рекламу (out), false — возврат (in)
	Cancel        bool
	Immediate     bool
	BreakDuration float64 // секунды, 0 если не указана
	AutoReturn    bool
}

// SpliceEvent — событие splicemonitor: анонс (pending) и наступление (occurred)
type SpliceEvent struct {
	EventID       int64
	Type          string // "out" / "in"
	Progress      string // pending / occurred / …
	TimeToEventMS int64  // время до события на момент анонса
	PreRollMS     *int64 // фактический pre-roll (в occurred, TSDuck >= 3.45)
	Count         int    // номер повтора анонса
	SplicePID     int
}

// Occurred — событие наступило (а не анонсировано заранее)
func (e *SpliceEvent) Occurred() bool {
	return e.Progress != "pending"
}

type xmlNode struct {
	Name  string          `json:"#name"`
	Nodes json.RawMessage `json:"#nodes"`
}

// ParseSCTE разбирает JSON splicemonitor
func ParseSCTE(payload string) (*SCTE35, error) {
	raw := json.RawMessage(payload)
	var head struct {
		Name  string            `json:"#name"`
		Nodes []json.RawMessage `json:"#nodes"`

		// event
		EventID       int64  `json:"event-id"`
		EventType     string `json:"event-type"`
		Progress      string `json:"progress"`
		TimeToEventMS int64  `json:"time-to-event-ms"`
		PreRollMS     *int64 `json:"pre-roll-ms"`
		Count         int    `json:"count"`
		SplicePID     int    `json:"splice-pid"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("scte json: %w", err)
	}

	msg := &SCTE35{Raw: raw}
	switch head.Name {
	case "event":
		msg.IsEvent = true
		msg.Event = &SpliceEvent{
			EventID:       head.EventID,
			Type:          head.EventType,
			Progress:      head.Progress,
			TimeToEventMS: head.TimeToEventMS,
			PreRollMS:     head.PreRollMS,
			Count:         head.Count,
			SplicePID:     head.SplicePID,
		}
	case "splice_information_table":
		msg.IsTable = true
		for _, n := range head.Nodes {
			if err := msg.parseTableNode(n); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("scte json: unknown #name %q", head.Name)
	}
	return msg, nil
}

func (m *SCTE35) parseTableNode(n json.RawMessage) error {
	var node xmlNode
	if err := json.Unmarshal(n, &node); err != nil {
		return fmt.Errorf("scte node: %w", err)
	}
	switch {
	case node.Name == "metadata":
		var md struct {
			PID int `json:"pid"`
		}
		json.Unmarshal(n, &md)
		m.PID = md.PID
	case m.Command == "":
		// Первый узел после metadata — сама команда
		m.Command = node.Name
		if node.Name == "splice_insert" {
			return m.parseInsert(n)
		}
	default:
		m.Descriptors = append(m.Descriptors, node.Name)
	}
	return nil
}

func (m *SCTE35) parseInsert(n json.RawMessage) error {
	var si struct {
		EventID      int64             `json:"splice_event_id"`
		OutOfNetwork bool              `json:"out_of_network"`
		Cancel       bool              `json:"splice_event_cancel"`
		Immediate    bool              `json:"splice_immediate"`
		Nodes        []json.RawMessage `json:"#nodes"`
	}
	if err := json.Unmarshal(n, &si); err != nil {
		return fmt.Errorf("splice_insert: %w", err)
	}
	ins := &SpliceInsert{
		EventID:      si.EventID,
		OutOfNetwork: si.OutOfNetwork,
		Cancel:       si.Cancel,
		Immediate:    si.Immediate,
	}
	for _, c := range si.Nodes {
		var bd struct {
			Name       string `json:"#name"`
			Duration   int64  `json:"duration"` // такты 90 кГц
			AutoReturn bool   `json:"auto_return"`
		}
		if json.Unmarshal(c, &bd) == nil && bd.Name == "break_duration" {
			ins.BreakDuration = float64(bd.Duration) / 90000
			ins.AutoReturn = bd.AutoReturn
		}
	}
	m.Insert = ins
	return nil
}
