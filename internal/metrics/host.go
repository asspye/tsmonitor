package metrics

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// Счётчики UDP ядра: RcvbufErrors — датаграммы, выброшенные из-за переполнения
// буфера сокета на этом сервере. Отличают наши потери на приёме от потерь в сети.
var (
	descUDPIn     = prometheus.NewDesc("ts_host_udp_in_datagrams_total", "UDP datagrams received by the host (/proc/net/snmp InDatagrams)", nil, nil)
	descUDPInErr  = prometheus.NewDesc("ts_host_udp_in_errors_total", "UDP receive errors on the host (/proc/net/snmp InErrors)", nil, nil)
	descUDPRcvbuf = prometheus.NewDesc("ts_host_udp_rcvbuf_errors_total", "UDP datagrams dropped because a socket receive buffer was full (/proc/net/snmp RcvbufErrors)", nil, nil)
)

const procNetSNMP = "/proc/net/snmp"

func collectHost(ch chan<- prometheus.Metric) {
	udp, err := readUDPStats(procNetSNMP)
	if err != nil {
		return // не Linux или нет доступа — просто без этих метрик
	}
	emit := func(d *prometheus.Desc, key string) {
		if v, ok := udp[key]; ok {
			ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v)
		}
	}
	emit(descUDPIn, "InDatagrams")
	emit(descUDPInErr, "InErrors")
	emit(descUDPRcvbuf, "RcvbufErrors")
}

// readUDPStats читает пару строк "Udp:" (заголовки и значения) из /proc/net/snmp
func readUDPStats(path string) (map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var header []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || fields[0] != "Udp:" {
			continue
		}
		if header == nil {
			header = fields[1:]
			continue
		}
		res := make(map[string]float64, len(header))
		for i, name := range header {
			if i+1 < len(fields) {
				if v, err := strconv.ParseFloat(fields[i+1], 64); err == nil {
					res[name] = v
				}
			}
		}
		return res, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, os.ErrNotExist
}
