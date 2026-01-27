package tsp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Регулярные выражения для парсинга
var (
	bitrateRegex          = regexp.MustCompile(`TS bitrate: ([\d,]+) bits/s, net bitrate: ([\d,]+) bits/s`)
	elementaryStreamRegex = regexp.MustCompile(`Elementary stream: type (0x[0-9A-F]+) \(([^)]+)\), PID: (0x[0-9A-F]+) \((\d+)\)`)
	languageRegex         = regexp.MustCompile(`Language: (\w+), Type:`)
	subtitleRegex         = regexp.MustCompile(`- Descriptor \d+: Subtitling`)
	serviceRegex          = regexp.MustCompile(`Service: "([^"]+)", Provider: "([^"]*)"`)
	tsidRegex             = regexp.MustCompile(`Transport Stream Id: (0x[0-9A-F]+) \((\d+)\)`)
	serviceTypeRegex      = regexp.MustCompile(`Service type: (0x[0-9A-F]+) \(([^)]+)\)`)
)

// ParseOutput парсит вывод tsp команды и возвращает StreamMetrics
func ParseOutput(output string, streamURL string, description string) (*StreamMetrics, error) {
	metrics := &StreamMetrics{
		StreamURL:   streamURL,
		Description: description,
		LastSeen:    time.Now(),
		PIDs:        []PIDInfo{},
		CCErrors:    make(map[string]int64),
	}

	// Парсим битрейт
	if err := parseBitrate(output, metrics); err != nil {
		return nil, fmt.Errorf("failed to parse bitrate: %w", err)
	}

	// Парсим PIDs
	if err := parsePIDs(output, metrics); err != nil {
		return nil, fmt.Errorf("failed to parse PIDs: %w", err)
	}

	// Парсим service info
	parseServiceInfo(output, metrics)

	// Обновляем статус
	metrics.UpdateStatus()

	return metrics, nil
}

// parseBitrate извлекает информацию о битрейте
func parseBitrate(output string, metrics *StreamMetrics) error {
	matches := bitrateRegex.FindStringSubmatch(output)
	if len(matches) == 0 {
		return nil
	}

	totalStr := strings.ReplaceAll(matches[1], ",", "")
	netStr := strings.ReplaceAll(matches[2], ",", "")

	total, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse total bitrate: %w", err)
	}

	net, err := strconv.ParseInt(netStr, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse net bitrate: %w", err)
	}

	metrics.Bitrate.TotalBPS = total
	metrics.Bitrate.NetBPS = net

	return nil
}

// parsePIDs извлекает информацию о PIDs
func parsePIDs(output string, metrics *StreamMetrics) error {
	lines := strings.Split(output, "\n")
	
	// Используем map для дедупликации PIDs
	pidMap := make(map[string]PIDInfo)

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Ищем Elementary stream
		matches := elementaryStreamRegex.FindStringSubmatch(line)
		if len(matches) == 0 {
			continue
		}

		streamType := matches[1]
		streamDesc := matches[2]
		pidHex := matches[3]
		pidDec := matches[4]

		// Пропускаем если этот PID уже есть
		if _, exists := pidMap[pidHex]; exists {
			continue
		}

		pidDecInt, _ := strconv.Atoi(pidDec)

		pid := PIDInfo{
			PID:        pidHex,
			PIDDecimal: pidDecInt,
			Type:       GetPIDType(streamType),
			Codec:      StreamTypeMap[streamType],
		}

		if pid.Codec == "" {
			pid.Codec = strings.ToLower(strings.ReplaceAll(streamDesc, " ", "_"))
		}

		// Ищем язык и субтитры в следующих строках
		for j := i + 1; j < len(lines); j++ {
			if strings.Contains(lines[j], "Elementary stream:") {
				break
			}

			if strings.Contains(lines[j], "Language:") && pid.Language == "" {
				langMatches := languageRegex.FindStringSubmatch(lines[j])
				if len(langMatches) > 0 {
					pid.Language = langMatches[1]
				}
			}

			if strings.Contains(lines[j], "Subtitling") && !pid.IsSubtitle {
				pid.IsSubtitle = true
				pid.SubtitleType = "dvb_subtitle"
				if j+1 < len(lines) && strings.Contains(lines[j+1], "Language:") {
					langMatches := languageRegex.FindStringSubmatch(lines[j+1])
					if len(langMatches) > 0 {
						pid.Language = langMatches[1]
					}
				}
			}
		}

		pidMap[pidHex] = pid
	}

	// Конвертируем map в slice
	for _, pid := range pidMap {
		metrics.PIDs = append(metrics.PIDs, pid)
	}

	return nil
}

// parseServiceInfo извлекает информацию о сервисе из SDT
func parseServiceInfo(output string, metrics *StreamMetrics) {
	serviceMatches := serviceRegex.FindStringSubmatch(output)
	if len(serviceMatches) > 0 {
		metrics.ServiceInfo.ServiceName = serviceMatches[1]
		metrics.ServiceInfo.Provider = serviceMatches[2]
	}

	tsidMatches := tsidRegex.FindStringSubmatch(output)
	if len(tsidMatches) > 0 {
		metrics.ServiceInfo.TSID = tsidMatches[1]
		metrics.TSID = tsidMatches[1]
	}

	typeMatches := serviceTypeRegex.FindStringSubmatch(output)
	if len(typeMatches) > 0 {
		serviceType := typeMatches[2]
		if strings.Contains(strings.ToUpper(serviceType), "HD") {
			metrics.ServiceInfo.ServiceType = "HD"
		} else if strings.Contains(strings.ToUpper(serviceType), "SD") {
			metrics.ServiceInfo.ServiceType = "SD"
		} else {
			metrics.ServiceInfo.ServiceType = "SD"
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
