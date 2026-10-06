package metrics

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/otcnet/tsmonitor/internal/stream"
)

func TestCollector(t *testing.T) {
	b, err := os.ReadFile("../tsp/testdata/analyze-233.32.71.45.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	st := stream.New("233.32.71.45:5000", "МАТЧ! | основа", 5*time.Second)
	st.HandleLine("* analyze: AN:"+strings.TrimSpace(string(b)), now, nil)
	st.HandleLine("* iat: IAT: 569 microseconds (std.dev: 45, min: 90, max: 1057), source: kernel, pkt/dgram: 7 (min: 7, max: 7)", now, nil)
	st.HandleLine(`* splicemonitor: SCTE:{"#name":"splice_information_table","#nodes":[{"#name":"metadata","pid":300},{"#name":"splice_null"}]}`, now, nil)

	c := NewCollector([]Source{{State: st, PCRMonitored: true}})
	c.now = func() time.Time { return now.Add(time.Second) }

	problems, err := testutil.CollectAndLint(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		if p.Metric == "ts_stream_pid_count" {
			continue // имя из прежней версии, на нём держатся дашборды
		}
		t.Errorf("lint %s: %s", p.Metric, p.Text)
	}

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err) // в том числе дубликаты серий
	}
	have := map[string]int{}
	for _, mf := range mfs {
		have[mf.GetName()] = len(mf.GetMetric())
	}
	for name, n := range map[string]int{
		"ts_stream_status":                    1,
		"ts_stream_bitrate_bps":               2,
		"ts_stream_pid_count":                 4,
		"ts_stream_pid_info":                  3,
		"ts_stream_service_info":              1,
		"ts_stream_iat_seconds":               4,
		"ts_stream_pcr_jitter_exceeded_total": 1,
		"ts_stream_scte35_commands_total":     1,
		"ts_stream_scte35_events_total":       2,
		"ts_stream_scte35_break_active":       1,
	} {
		if have[name] != n {
			t.Errorf("%s: %d series, want %d", name, have[name], n)
		}
	}
	if have["ts_stream_cc_errors_total"] < 3 || have["ts_stream_table_interval_seconds"] < 8 {
		t.Errorf("cc=%d tables=%d", have["ts_stream_cc_errors_total"], have["ts_stream_table_interval_seconds"])
	}
}
