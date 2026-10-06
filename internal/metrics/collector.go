// Package metrics строит метрики Prometheus из снимков состояния потоков.
// Имена и метки метрик прежней версии (ts_stream_status, ts_stream_bitrate_bps,
// ts_stream_pid_count, ts_stream_pid_info, ts_stream_service_info,
// ts_stream_cc_errors_total) сохранены.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/otcnet/tsmonitor/internal/stream"
)

// Source — поток, из которого собираются метрики
type Source struct {
	State        *stream.State
	PCRMonitored bool // для потока запущен pcrverify
}

// Collector — prometheus.Collector по всем потокам
type Collector struct {
	sources []Source
	now     func() time.Time
}

// NewCollector создаёт коллектор
func NewCollector(sources []Source) *Collector {
	return &Collector{sources: sources, now: time.Now}
}

var base = []string{"stream", "description"}

func desc(name, help string, extra ...string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, append(append([]string{}, base...), extra...), nil)
}

var (
	descStatus       = desc("ts_stream_status", "Stream status (1 = online, 0 = offline)")
	descBitrate      = desc("ts_stream_bitrate_bps", "Stream bitrate in bits per second (net = without null packets)", "type")
	descPIDCount     = desc("ts_stream_pid_count", "Number of elementary stream PIDs by type", "type")
	descPIDInfo      = desc("ts_stream_pid_info", "PID information (value always 1, info in labels)", "pid", "type", "codec", "language", "resolution")
	descPIDBitrate   = desc("ts_stream_pid_bitrate_bps", "Elementary stream PID bitrate in bits per second", "pid", "type")
	descServiceInfo  = desc("ts_stream_service_info", "Service information (value always 1, info in labels)", "service_name", "provider", "service_type")
	descCCErrors     = desc("ts_stream_cc_errors_total", "Total number of continuity counter errors by PID", "pid")
	descTEI          = desc("ts_stream_transport_errors_total", "Total number of TS packets with transport_error_indicator set")
	descSync         = desc("ts_stream_sync_errors_total", "Total number of TS packets with invalid sync byte")
	descTable        = desc("ts_stream_table_interval_seconds", "PSI/SI table repetition interval over the last analysis interval", "table", "stat")
	descIAT          = desc("ts_stream_iat_seconds", "UDP datagram inter-arrival time over the last interval (stddev and max show network jitter)", "stat")
	descPCRExceeded  = desc("ts_stream_pcr_jitter_exceeded_total", "Total number of PCRs whose arrival jitter exceeded pcr_jitter_max")
	descPCRMax       = desc("ts_stream_pcr_jitter_max_seconds", "Largest PCR arrival jitter above pcr_jitter_max in the last interval (0 = none)")
	descLastReport   = desc("ts_stream_last_report_timestamp_seconds", "Time of the last analyze report from tsp")
	descRestarts     = desc("ts_stream_restarts_total", "Number of tsp process restarts")
	descParseErrors  = desc("ts_stream_parse_errors_total", "Number of tsp output lines that failed to parse")
	descSCTECommands = desc("ts_stream_scte35_commands_total", "SCTE-35 splice commands received (splice_null included)", "command")
	descSCTELastCmd  = desc("ts_stream_scte35_last_command_timestamp_seconds", "Time of the last SCTE-35 section of any command")
	descSCTEEvents   = desc("ts_stream_scte35_events_total", "SCTE-35 splice events that occurred (out = ad break start, in = return)", "direction")
	descSCTELastEvt  = desc("ts_stream_scte35_last_event_timestamp_seconds", "Time of the last occurred SCTE-35 splice event", "direction")
	descSCTEPreroll  = desc("ts_stream_scte35_preroll_seconds", "Pre-roll of the last splice event: time between the first announcement and the event", "direction")
	descSCTEActive   = desc("ts_stream_scte35_break_active", "1 while an SCTE-35 ad break is in progress")
	descSCTEBreakDur = desc("ts_stream_scte35_break_duration_seconds", "Announced duration of the current or last ad break (0 = not announced)")
)

// Describe — коллектор «unchecked»: набор серий меняется вместе с потоками
func (c *Collector) Describe(chan<- *prometheus.Desc) {}

// Collect строит метрики из снимков
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	now := c.now()
	for _, src := range c.sources {
		s := src.State.Snapshot(now, src.PCRMonitored)
		collectStream(ch, &s)
	}
	collectHost(ch)
}

func gauge(ch chan<- prometheus.Metric, d *prometheus.Desc, v float64, labels ...string) {
	ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
}

func counter(ch chan<- prometheus.Metric, d *prometheus.Desc, v float64, labels ...string) {
	ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v, labels...)
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func unixSeconds(t time.Time) float64 {
	return float64(t.UnixNano()) / 1e9
}

func collectStream(ch chan<- prometheus.Metric, s *stream.Snapshot) {
	l := []string{s.URL, s.Description}
	with := func(extra ...string) []string { return append(append([]string{}, l...), extra...) }

	gauge(ch, descStatus, boolValue(s.Online), l...)
	gauge(ch, descBitrate, float64(s.BitrateTotal), with("total")...)
	gauge(ch, descBitrate, float64(s.BitrateNet), with("net")...)

	counts := map[string]int{"video": 0, "audio": 0, "data": 0, "other": 0}
	for _, p := range s.PIDs {
		counts[p.Type]++
		lang := p.Language
		if lang == "" {
			lang = "none"
		}
		gauge(ch, descPIDInfo, 1, with(p.PID, p.Type, p.Codec, lang, p.Resolution)...)
		gauge(ch, descPIDBitrate, float64(p.BitrateBPS), with(p.PID, p.Type)...)
	}
	for t, n := range counts {
		gauge(ch, descPIDCount, float64(n), with(t)...)
	}

	for _, sv := range s.Services {
		gauge(ch, descServiceInfo, 1, with(sv.Name, sv.Provider, sv.Type)...)
	}

	for pid, n := range s.CCErrors {
		counter(ch, descCCErrors, n, with(pid)...)
	}
	counter(ch, descTEI, s.TransportErrors, l...)
	counter(ch, descSync, s.SyncErrors, l...)

	for name, t := range s.Tables {
		gauge(ch, descTable, t.AvgSeconds, with(name, "avg")...)
		gauge(ch, descTable, t.MaxSeconds, with(name, "max")...)
	}

	if s.IAT != nil {
		gauge(ch, descIAT, float64(s.IAT.MeanUS)/1e6, with("mean")...)
		gauge(ch, descIAT, float64(s.IAT.StdDevUS)/1e6, with("stddev")...)
		gauge(ch, descIAT, float64(s.IAT.MinUS)/1e6, with("min")...)
		gauge(ch, descIAT, float64(s.IAT.MaxUS)/1e6, with("max")...)
	}

	if s.PCRMonitored {
		counter(ch, descPCRExceeded, s.PCRExceeded, l...)
		gauge(ch, descPCRMax, float64(s.PCRMaxUS)/1e6, l...)
	}

	if !s.LastReport.IsZero() {
		gauge(ch, descLastReport, unixSeconds(s.LastReport), l...)
	}
	counter(ch, descRestarts, s.Restarts, l...)
	counter(ch, descParseErrors, s.ParseErrors, l...)

	collectSCTE(ch, &s.SCTE, with)
}

func collectSCTE(ch chan<- prometheus.Metric, sc *stream.SCTESnapshot, with func(...string) []string) {
	if !sc.Seen {
		return
	}
	for cmd, n := range sc.Commands {
		counter(ch, descSCTECommands, n, with(cmd)...)
	}
	if !sc.LastCommand.IsZero() {
		gauge(ch, descSCTELastCmd, unixSeconds(sc.LastCommand), with()...)
	}
	for _, dir := range []string{"out", "in"} {
		counter(ch, descSCTEEvents, sc.Events[dir], with(dir)...)
		if t, ok := sc.LastEvent[dir]; ok {
			gauge(ch, descSCTELastEvt, unixSeconds(t), with(dir)...)
		}
		if p, ok := sc.Preroll[dir]; ok {
			gauge(ch, descSCTEPreroll, p, with(dir)...)
		}
	}
	gauge(ch, descSCTEActive, boolValue(sc.BreakActive), with()...)
	gauge(ch, descSCTEBreakDur, sc.BreakDuration, with()...)
}
