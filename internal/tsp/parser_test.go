package tsp

import (
	"testing"
)

// Тестовые данные из реальных потоков
const (
	// Поток с битрейтом и языками
	testOutput1 = `* bitrate_monitor: 2026/01/26 22:38:39, TS bitrate: 5,077,945 bits/s, net bitrate: 4,758,039 bits/s

* SDT Actual, TID 0x42 (66), PID 0x0011 (17)
  Transport Stream Id: 0x000C (12)
  Service: "Silk Way", Provider: "OTCNET"
  Service type: 0x19 (Advanced codec HD digital television service)

* PMT, TID 0x02 (2), PID 0x012E (302)
  Program: 0x03E8 (1000), PCR PID: 0x0066 (102)
  Elementary stream: type 0x1B (AVC video), PID: 0x0066 (102)
  Elementary stream: type 0x03 (MPEG-1 Audio), PID: 0x00CA (202)
  - Descriptor 0: ISO-639 Language (0x0A, 10), 4 bytes
    Language: rus, Type: 0x00 (undefined)
  Elementary stream: type 0x03 (MPEG-1 Audio), PID: 0x012F (303)
  - Descriptor 0: ISO-639 Language (0x0A, 10), 4 bytes
    Language: kaz, Type: 0x00 (undefined)`
)

func TestParseBitrate(t *testing.T) {
	metrics := &StreamMetrics{}
	err := parseBitrate(testOutput1, metrics)
	if err != nil {
		t.Fatalf("parseBitrate() error = %v", err)
	}

	if metrics.Bitrate.TotalBPS != 5077945 {
		t.Errorf("TotalBPS = %d, want 5077945", metrics.Bitrate.TotalBPS)
	}

	if metrics.Bitrate.NetBPS != 4758039 {
		t.Errorf("NetBPS = %d, want 4758039", metrics.Bitrate.NetBPS)
	}
}

func TestParsePIDs(t *testing.T) {
	metrics := &StreamMetrics{PIDs: []PIDInfo{}}
	err := parsePIDs(testOutput1, metrics)
	if err != nil {
		t.Fatalf("parsePIDs() error = %v", err)
	}

	if len(metrics.PIDs) != 3 {
		t.Fatalf("PIDs count = %d, want 3", len(metrics.PIDs))
	}

	// Проверяем видео
	if metrics.PIDs[0].Type != "video" {
		t.Errorf("PID[0] type = %s, want video", metrics.PIDs[0].Type)
	}
	if metrics.PIDs[0].Codec != "h264" {
		t.Errorf("PID[0] codec = %s, want h264", metrics.PIDs[0].Codec)
	}

	// Проверяем аудио с языками
	if metrics.PIDs[1].Type != "audio" {
		t.Errorf("PID[1] type = %s, want audio", metrics.PIDs[1].Type)
	}
	if metrics.PIDs[1].Language != "rus" {
		t.Errorf("PID[1] language = %s, want rus", metrics.PIDs[1].Language)
	}

	if metrics.PIDs[2].Language != "kaz" {
		t.Errorf("PID[2] language = %s, want kaz", metrics.PIDs[2].Language)
	}
}

func TestParseServiceInfo(t *testing.T) {
	metrics := &StreamMetrics{}
	parseServiceInfo(testOutput1, metrics)

	if metrics.ServiceInfo.ServiceName != "Silk Way" {
		t.Errorf("ServiceName = %s, want Silk Way", metrics.ServiceInfo.ServiceName)
	}

	if metrics.ServiceInfo.Provider != "OTCNET" {
		t.Errorf("Provider = %s, want OTCNET", metrics.ServiceInfo.Provider)
	}

	if metrics.ServiceInfo.ServiceType != "HD" {
		t.Errorf("ServiceType = %s, want HD", metrics.ServiceInfo.ServiceType)
	}

	if metrics.TSID != "0x000C" {
		t.Errorf("TSID = %s, want 0x000C", metrics.TSID)
	}
}

func TestParseOutput(t *testing.T) {
	metrics, err := ParseOutput(testOutput1, "233.198.134.1:3333", "Test Stream")
	if err != nil {
		t.Fatalf("ParseOutput() error = %v", err)
	}

	// Проверяем базовые поля
	if metrics.StreamURL != "233.198.134.1:3333" {
		t.Errorf("StreamURL = %s, want 233.198.134.1:3333", metrics.StreamURL)
	}

	// Проверяем битрейт
	if metrics.Bitrate.TotalBPS != 5077945 {
		t.Errorf("TotalBPS = %d, want 5077945", metrics.Bitrate.TotalBPS)
	}

	// Проверяем PIDs
	if len(metrics.PIDs) != 3 {
		t.Errorf("PIDs count = %d, want 3", len(metrics.PIDs))
	}

	// Проверяем статус
	if !metrics.Status {
		t.Errorf("Status = false, want true")
	}
}
