package stream

import (
	"fmt"
	"sort"
	"strings"

	"github.com/otcnet/tsmonitor/internal/eventlog"
)

// message — короткое описание события по-русски для журнала в Grafana
// (поле "message"; полный набор полей остаётся в JSON)
func message(kind string, f eventlog.Fields) string {
	switch kind {
	case KindCCErrors:
		pids := pidCounts(f["pids"])
		tei := ""
		if n, ok := f["transport_errors"].(int64); ok && n > 0 {
			tei = fmt.Sprintf(", TEI %d", n)
		}
		if f["phase"] == "start" {
			return "CC-ошибки начались: " + pids + tei
		}
		reason := ""
		if f["reason"] == "stream_down" {
			reason = " (поток пропал)"
		}
		return fmt.Sprintf("CC-ошибки закончились%s: %v за %s — %s%s",
			reason, f["total"], seconds(f["duration_s"]), pids, tei)

	case KindPIDsChanged:
		var parts []string
		if added, ok := f["added"].([]map[string]string); ok {
			for _, p := range added {
				parts = append(parts, "добавлен "+pidText(p))
			}
		}
		if removed, ok := f["removed"].([]map[string]string); ok {
			for _, p := range removed {
				parts = append(parts, "пропал "+pidText(p))
			}
		}
		if changed, ok := f["changed"].([]map[string]any); ok {
			for _, c := range changed {
				from, _ := c["from"].(map[string]string)
				to, _ := c["to"].(map[string]string)
				parts = append(parts, fmt.Sprintf("%s: %s → %s", c["pid"], pidAttrs(from), pidAttrs(to)))
			}
		}
		return "Состав PID изменился: " + strings.Join(parts, "; ")

	case KindServiceChanged:
		from, _ := f["from"].([]string)
		to, _ := f["to"].([]string)
		return fmt.Sprintf("Сервис изменился: %s → %s", strings.Join(from, ", "), strings.Join(to, ", "))

	case KindStreamDown:
		switch f["reason"] {
		case "zero bitrate":
			return "Поток пропал: нулевой битрейт"
		case "no data since monitor start":
			return "Потока нет с запуска мониторинга"
		}
		return "Поток пропал: нет данных"

	case KindStreamUp:
		return "Поток вернулся, не было " + seconds(f["down_duration_s"])

	case KindTSPExit:
		return fmt.Sprintf("tsp завершился (%v), перезапусков: %v", f["error"], f["exits"])

	case KindSCTE35:
		return scteMessage(f)
	}
	return kind
}

func scteMessage(f eventlog.Fields) string {
	if f["type"] == "event" {
		what := "начало рекламы (out)"
		if f["event_type"] == "in" {
			what = "конец рекламы (in)"
		}
		if f["progress"] == "pending" {
			ms, _ := f["time_to_event_ms"].(int64)
			return fmt.Sprintf("SCTE-35: анонс #%v — %s через %s, id %v", f["count"], what, seconds(float64(ms)/1000), f["event_id"])
		}
		s := fmt.Sprintf("SCTE-35: %s, id %v", what, f["event_id"])
		if ms, ok := f["pre_roll_ms"].(int64); ok {
			s += ", pre-roll " + seconds(float64(ms)/1000)
		}
		return s
	}
	cmd, _ := f["command"].(string)
	s := "SCTE-35: " + cmd
	if out, ok := f["out_of_network"].(bool); ok {
		if out {
			s += " out"
		} else {
			s += " in"
		}
	}
	if d, ok := f["break_duration_s"].(float64); ok {
		s += ", блок " + seconds(d)
	}
	if id, ok := f["event_id"]; ok {
		s += fmt.Sprintf(", id %v", id)
	}
	if ds, ok := f["descriptors"].([]string); ok {
		s += " (" + strings.Join(ds, ", ") + ")"
	}
	if f["cancel"] == true {
		s += ", отмена"
	}
	return s
}

func pidCounts(v any) string {
	m, _ := v.(map[string]int64)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func pidText(p map[string]string) string {
	return p["pid"] + " " + pidAttrs(p)
}

func pidAttrs(p map[string]string) string {
	parts := []string{p["codec"]}
	for _, k := range []string{"language", "resolution"} {
		if p[k] != "" {
			parts = append(parts, p[k])
		}
	}
	return strings.Join(parts, " ")
}

func seconds(v any) string {
	d, _ := v.(float64)
	if d >= 10 || d == float64(int64(d)) {
		return fmt.Sprintf("%.0f с", d)
	}
	return fmt.Sprintf("%.1f с", d)
}
