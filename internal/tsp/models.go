package tsp

import "time"

// StreamMetrics содержит все метрики для одного потока
type StreamMetrics struct {
	StreamURL   string
	Description string
	Status      bool      // online/offline
	LastSeen    time.Time
	Bitrate     BitrateInfo
	PIDs        []PIDInfo
	ServiceInfo ServiceInfo
	CCErrors    map[string]int64 // PID -> error count
	TSID        string           // Transport Stream ID
}

// BitrateInfo содержит информацию о битрейте
type BitrateInfo struct {
	TotalBPS int64 // Total TS bitrate (bits per second)
	NetBPS   int64 // Net bitrate (payload only)
}

// PIDInfo содержит информацию о PID
type PIDInfo struct {
	PID          string
	PIDDecimal   int    // PID в десятичном формате
	Type         string // video, audio, data, other
	Codec        string // h264, mpeg1audio, mpeg2audio, aac, etc
	Language     string // rus, eng, kaz, kir, uzb, etc (optional)
	IsSubtitle   bool   // true если это субтитры
	SubtitleType string // DVB subtitles, teletext, etc
}

// ServiceInfo содержит информацию о сервисе из SDT
type ServiceInfo struct {
	ServiceName string
	Provider    string
	TSID        string // Transport Stream ID
	ServiceType string // HD/SD/etc
}

// StreamTypeMap маппинг stream_type на названия кодеков
var StreamTypeMap = map[string]string{
	// Video
	"0x1B": "h264",       // AVC video
	"0x10": "mpeg4video", // MPEG-4 Video
	"0x24": "hevc",       // HEVC/H.265
	"0x02": "mpeg2video", // MPEG-2 Video

	// Audio
	"0x03": "mpeg1audio", // MPEG-1 Audio
	"0x04": "mpeg2audio", // MPEG-2 Audio
	"0x0F": "aac",        // MPEG-2 AAC Audio
	"0x11": "aac_latm",   // MPEG-4 AAC LATM
	"0x81": "ac3",        // AC-3 audio
	"0x87": "eac3",       // Enhanced AC-3

	// Data/Subtitles
	"0x06": "private", // Private data (subtitles, teletext)
}

// GetPIDType определяет тип PID по stream_type
func GetPIDType(streamType string) string {
	// Видео
	if streamType == "0x1B" || streamType == "0x10" || streamType == "0x24" || streamType == "0x02" {
		return "video"
	}

	// Аудио
	if streamType == "0x03" || streamType == "0x04" || streamType == "0x0F" ||
		streamType == "0x11" || streamType == "0x81" || streamType == "0x87" {
		return "audio"
	}

	// Private data (обычно субтитры)
	if streamType == "0x06" {
		return "data"
	}

	// Неизвестный тип
	return "other"
}

// UpdateStatus обновляет статус потока на основе метрик
func (s *StreamMetrics) UpdateStatus() {
	// Поток считается offline если:
	// 1. Битрейт = 0
	// 2. Нет данных > 5 секунд
	// 3. Нет PID информации

	timeout := 5 * time.Second
	now := time.Now()

	s.Status = s.Bitrate.TotalBPS > 0 &&
		now.Sub(s.LastSeen) < timeout &&
		len(s.PIDs) > 0
}
