package tsp

import (
	"regexp"
	"strings"
)

// AnalyzeReport — отчёт плагина analyze за один интервал (--json-line).
// analyze с --interval сбрасывает статистику после каждого отчёта,
// поэтому счётчики (discontinuities, transport-errors) — приращения за интервал.
type AnalyzeReport struct {
	TS struct {
		ID      int   `json:"id"`
		Bitrate int64 `json:"bitrate"`
		Packets struct {
			Total           int64 `json:"total"`
			InvalidSyncs    int64 `json:"invalid-syncs"`
			TransportErrors int64 `json:"transport-errors"`
		} `json:"packets"`
	} `json:"ts"`
	PIDs     []AnalyzePID     `json:"pids"`
	Services []AnalyzeService `json:"services"`
	Tables   []AnalyzeTable   `json:"tables"`
}

// AnalyzePID — один PID из отчёта analyze
type AnalyzePID struct {
	ID           int    `json:"id"`
	Description  string `json:"description"`
	Bitrate      int64  `json:"bitrate"`
	Audio        bool   `json:"audio"`
	Video        bool   `json:"video"`
	Global       bool   `json:"global"`
	PMT          bool   `json:"pmt"`
	ECM          bool   `json:"ecm"`
	EMM          bool   `json:"emm"`
	IsScrambled  bool   `json:"is-scrambled"`
	Language     string `json:"language"`
	ServiceCount int    `json:"service-count"`
	Packets      struct {
		Total           int64 `json:"total"`
		Discontinuities int64 `json:"discontinuities"`
		Duplicated      int64 `json:"duplicated"`
	} `json:"packets"`
}

// AnalyzeService — один сервис (из PMT/SDT)
type AnalyzeService struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	TypeName string `json:"type-name"`
	TSID     int    `json:"tsid"`
}

// AnalyzeTable — PSI/SI таблица и интервалы её повторения
type AnalyzeTable struct {
	PID              int   `json:"pid"`
	TID              int   `json:"tid"`
	RepetitionMS     int64 `json:"repetition-ms"`
	MaxRepetitionMS  int64 `json:"max-repetition-ms"`
	MinRepetitionMS  int64 `json:"min-repetition-ms"`
	SectionsReceived int64 `json:"sections"`
}

// StuffingPID — PID нулевых пакетов
const StuffingPID = 0x1FFF

// TableNames — таблицы, для которых экспортируются интервалы повторения (TID → имя)
var TableNames = map[int]string{
	0x00: "PAT",
	0x01: "CAT",
	0x02: "PMT",
	0x40: "NIT",
	0x42: "SDT",
}

// IsElementary сообщает, что PID — элементарный поток сервиса
// (видео, аудио, субтитры, SCTE-35…), а не PSI/SI, ECM/EMM или stuffing.
// PID с флагом pmt, но с видео/аудио — ошибка источника (PMT одного сервиса
// на PID элементарного потока другого); такой PID всё равно считаем потоком.
func (p *AnalyzePID) IsElementary() bool {
	if p.Global || p.ECM || p.EMM || p.ID == StuffingPID || p.ServiceCount == 0 {
		return false
	}
	return !p.PMT || p.Video || p.Audio
}

// IATReport — строка плагина iat: интервалы между UDP-датаграммами за период
type IATReport struct {
	MeanUS   int64
	StdDevUS int64
	MinUS    int64
	MaxUS    int64
}

// PCRJitter — одно нарушение порога из pcrverify
type PCRJitter struct {
	PID      int
	JitterUS int64 // модуль джиттера, мкс
}

// codecByPrefix — начало описания TSDuck → имя кодека.
// Имена совпадают с прежними значениями метки codec (по stream_type),
// чтобы не ломать дашборды. Порядок важен: более длинные префиксы раньше.
var codecByPrefix = []struct{ prefix, codec string }{
	{"AVC video", "h264"},
	{"HEVC video", "hevc"},
	{"VVC video", "vvc"},
	{"MPEG-2 Video", "mpeg2video"},
	{"MPEG-1 Video", "mpeg1video"},
	{"MPEG-4 Video", "mpeg4video"},
	{"MPEG-1 Audio", "mpeg1audio"},
	{"MPEG-2 Audio", "mpeg2audio"},
	{"MPEG-2 AAC Audio", "aac"},
	{"MPEG-4 AAC Audio", "aac_latm"},
	{"MPEG-4 Audio", "aac_latm"},
	{"ATSC E-AC-3 Audio", "eac3"},
	{"E-AC-3 Audio", "eac3"},
	{"ATSC AC-3 Audio", "ac3"},
	{"AC-3 Audio", "ac3"},
	{"AC-4 Audio", "ac4"},
	{"DTS", "dts"},
	{"Teletext", "teletext"},
	{"Subtitles", "dvb_subtitle"},
	{"SCTE 35 Splice Info", "scte_35_splice_info"},
	{"MPEG-2 PES private data", "private"},
}

var resolutionRegex = regexp.MustCompile(`\b(\d{3,5}x\d{3,5})`)

// DescriptionHead — описание без деталей в скобках: "AVC video (1920x1080, …)" → "AVC video"
func DescriptionHead(desc string) string {
	if i := strings.Index(desc, " ("); i >= 0 {
		desc = desc[:i]
	}
	return strings.TrimSpace(desc)
}

// Codec определяет имя кодека по описанию PID
func Codec(desc string) string {
	head := DescriptionHead(desc)
	for _, c := range codecByPrefix {
		if strings.HasPrefix(head, c.prefix) {
			return c.codec
		}
	}
	if head == "" {
		return "unknown"
	}
	return strings.ToLower(strings.NewReplacer(" ", "_", "-", "_", ".", "").Replace(head))
}

// PIDType — video / audio / data / other, как в прежней версии
func PIDType(p *AnalyzePID) string {
	switch {
	case p.Video:
		return "video"
	case p.Audio:
		return "audio"
	}
	switch Codec(p.Description) {
	case "teletext", "dvb_subtitle", "private":
		return "data"
	}
	return "other"
}

// Resolution — разрешение видео из описания ("1920x1080") или ""
func Resolution(desc string) string {
	if m := resolutionRegex.FindStringSubmatch(desc); m != nil {
		return m[1]
	}
	return ""
}

// ServiceType — HD / SD по названию типа сервиса (как в прежней версии)
func ServiceType(typeName string) string {
	if strings.Contains(strings.ToUpper(typeName), "HD") {
		return "HD"
	}
	return "SD"
}

// CleanProvider — "(unknown)" из analyze превращаем в пустую строку, как было при разборе SDT
func CleanProvider(p string) string {
	if p == "(unknown)" {
		return ""
	}
	return p
}
