package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/otcnet/tsmonitor/internal/tsp"
)

// Exporter экспортирует метрики в Prometheus
type Exporter struct {
	streamStatus      *prometheus.GaugeVec
	streamBitrate     *prometheus.GaugeVec
	streamPIDCount    *prometheus.GaugeVec
	streamPIDInfo     *prometheus.GaugeVec
	streamServiceInfo *prometheus.GaugeVec
	streamCCErrors    *prometheus.CounterVec
}

// NewExporter создаёт новый экспортер метрик
func NewExporter() *Exporter {
	return &Exporter{
		streamStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ts_stream_status",
				Help: "Stream status (1 = online, 0 = offline)",
			},
			[]string{"stream", "description"},
		),

		streamBitrate: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ts_stream_bitrate_bps",
				Help: "Stream bitrate in bits per second",
			},
			[]string{"stream", "description", "type"},
		),

		streamPIDCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ts_stream_pid_count",
				Help: "Number of PIDs by type",
			},
			[]string{"stream", "description", "type"},
		),

		streamPIDInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ts_stream_pid_info",
				Help: "PID information (value always 1, info in labels)",
			},
			[]string{"stream", "description", "pid", "type", "codec", "language"},
		),

		streamServiceInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ts_stream_service_info",
				Help: "Service information (value always 1, info in labels)",
			},
			[]string{"stream", "description", "service_name", "provider", "service_type"},
		),

		streamCCErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ts_stream_cc_errors_total",
				Help: "Total number of continuity counter errors by PID",
			},
			[]string{"stream", "description", "pid"},
		),
	}
}

// Register регистрирует все метрики в Prometheus
func (e *Exporter) Register() error {
	if err := prometheus.Register(e.streamStatus); err != nil {
		return err
	}
	if err := prometheus.Register(e.streamBitrate); err != nil {
		return err
	}
	if err := prometheus.Register(e.streamPIDCount); err != nil {
		return err
	}
	if err := prometheus.Register(e.streamPIDInfo); err != nil {
		return err
	}
	if err := prometheus.Register(e.streamServiceInfo); err != nil {
		return err
	}
	if err := prometheus.Register(e.streamCCErrors); err != nil {
		return err
	}
	return nil
}

// UpdateMetrics обновляет метрики на основе StreamMetrics
func (e *Exporter) UpdateMetrics(m *tsp.StreamMetrics) {
	stream := m.StreamURL
	desc := m.Description

	// Обновляем статус
	var status float64
	if m.Status {
		status = 1
	}
	e.streamStatus.WithLabelValues(stream, desc).Set(status)

	// Обновляем битрейт
	e.streamBitrate.WithLabelValues(stream, desc, "total").Set(float64(m.Bitrate.TotalBPS))
	e.streamBitrate.WithLabelValues(stream, desc, "net").Set(float64(m.Bitrate.NetBPS))

	// Подсчитываем PIDs по типам
	pidCounts := make(map[string]int)
	for _, pid := range m.PIDs {
		pidCounts[pid.Type]++
	}

	// Обновляем счётчики PIDs
	e.streamPIDCount.WithLabelValues(stream, desc, "video").Set(float64(pidCounts["video"]))
	e.streamPIDCount.WithLabelValues(stream, desc, "audio").Set(float64(pidCounts["audio"]))
	e.streamPIDCount.WithLabelValues(stream, desc, "data").Set(float64(pidCounts["data"]))
	e.streamPIDCount.WithLabelValues(stream, desc, "other").Set(float64(pidCounts["other"]))

	// Сбрасываем старые PID метрики для этого потока
	e.streamPIDInfo.DeletePartialMatch(prometheus.Labels{"stream": stream})

	// Обновляем информацию о каждом PID
	for _, pid := range m.PIDs {
		lang := pid.Language
		if lang == "" {
			lang = "none"
		}
		
		e.streamPIDInfo.WithLabelValues(
			stream,
			desc,
			pid.PID,
			pid.Type,
			pid.Codec,
			lang,
		).Set(1)
	}

	// Обновляем информацию о сервисе
	if m.ServiceInfo.ServiceName != "" {
		serviceType := m.ServiceInfo.ServiceType
		if serviceType == "" {
			serviceType = "unknown"
		}
		
		e.streamServiceInfo.WithLabelValues(
			stream,
			desc,
			m.ServiceInfo.ServiceName,
			m.ServiceInfo.Provider,
			serviceType,
		).Set(1)
	}

	// КРИТИЧНО: Инициализируем CC Errors счетчики для всех PIDs в 0
	// Это позволяет Prometheus видеть метрику даже когда ошибок нет
	for _, pid := range m.PIDs {
		// Add(0) создаст метрику если её нет, или ничего не сделает если есть
		e.streamCCErrors.WithLabelValues(stream, desc, pid.PID).Add(0)
	}

	// Обновляем ТОЛЬКО если есть новые ошибки
	for pid, errors := range m.CCErrors {
		if errors > 0 {
			e.streamCCErrors.WithLabelValues(stream, desc, pid).Add(float64(errors))
		}
	}
}

// ClearStreamMetrics очищает метрики для потока
func (e *Exporter) ClearStreamMetrics(streamURL string) {
	e.streamStatus.DeletePartialMatch(prometheus.Labels{"stream": streamURL})
	e.streamBitrate.DeletePartialMatch(prometheus.Labels{"stream": streamURL})
	e.streamPIDCount.DeletePartialMatch(prometheus.Labels{"stream": streamURL})
	e.streamPIDInfo.DeletePartialMatch(prometheus.Labels{"stream": streamURL})
	e.streamServiceInfo.DeletePartialMatch(prometheus.Labels{"stream": streamURL})
	e.streamCCErrors.DeletePartialMatch(prometheus.Labels{"stream": streamURL})
}
