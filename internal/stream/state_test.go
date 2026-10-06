package stream

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otcnet/tsmonitor/internal/eventlog"
)

type logged struct {
	kind   string
	fields eventlog.Fields
}

type memLog struct{ lines []logged }

func (m *memLog) Log(_, _, kind string, f eventlog.Fields) {
	m.lines = append(m.lines, logged{kind, f})
}

func (m *memLog) kinds(kind string) []eventlog.Fields {
	var res []eventlog.Fields
	for _, l := range m.lines {
		if l.kind == kind {
			res = append(res, l.fields)
		}
	}
	return res
}

func analyzeLine(t *testing.T) string {
	b, err := os.ReadFile("../tsp/testdata/analyze-233.32.71.45.json")
	if err != nil {
		t.Fatal(err)
	}
	return "* analyze: AN:" + strings.TrimSpace(string(b))
}

func TestAnalyzeOnlineAndStale(t *testing.T) {
	s := New("233.32.71.45:5000", "test", 5*time.Second)
	t0 := time.Unix(1_800_000_000, 0)

	if snap := s.Snapshot(t0, true); snap.Online {
		t.Fatal("must be offline before first report")
	}

	s.HandleLine(analyzeLine(t), t0, nil)
	snap := s.Snapshot(t0.Add(time.Second), true)
	if !snap.Online {
		t.Fatal("must be online after report")
	}
	if snap.BitrateTotal != 18500396 || snap.BitrateNet != 18500396-3474404 {
		t.Errorf("bitrate total=%d net=%d", snap.BitrateTotal, snap.BitrateNet)
	}
	if len(snap.PIDs) != 3 {
		t.Errorf("elementary PIDs = %d, want 3 (video, audio, teletext)", len(snap.PIDs))
	}
	if len(snap.Services) != 1 || snap.Services[0].Name != "No Name" {
		t.Errorf("services = %+v", snap.Services)
	}
	if _, ok := snap.CCErrors["0x00C8"]; !ok {
		t.Error("cc counter for video PID must exist even without errors")
	}
	if _, ok := snap.CCErrors["0x1FFF"]; ok {
		t.Error("no cc counter for stuffing")
	}
	if snap.Tables["PAT"].AvgSeconds <= 0 {
		t.Errorf("PAT interval = %+v", snap.Tables["PAT"])
	}

	stale := s.Snapshot(t0.Add(16*time.Second), true)
	if stale.Online || stale.BitrateTotal != 0 || len(stale.PIDs) != 0 {
		t.Errorf("stale snapshot must be offline with zero bitrate: %+v", stale)
	}
	if len(stale.Services) != 1 {
		t.Error("services are kept while offline")
	}
}

func TestCCAccumulates(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	now := time.Unix(1_800_000_000, 0)
	line := `* analyze: AN:{"ts":{"bitrate":1000,"packets":{"transport-errors":2,"invalid-syncs":1}},` +
		`"pids":[{"id":256,"service-count":1,"video":true,"description":"AVC video","packets":{"discontinuities":3}}],"services":[],"tables":[]}`
	s.HandleLine(line, now, nil)
	s.HandleLine(line, now.Add(5*time.Second), nil)
	snap := s.Snapshot(now.Add(6*time.Second), true)
	if snap.CCErrors["0x0100"] != 6 || snap.TransportErrors != 4 || snap.SyncErrors != 2 {
		t.Errorf("cc=%v tei=%v sync=%v", snap.CCErrors, snap.TransportErrors, snap.SyncErrors)
	}

	// PID пропал из потока больше часа назад — счётчик забывается
	empty := `* analyze: AN:{"ts":{"bitrate":1000},"pids":[],"services":[],"tables":[]}`
	s.HandleLine(empty, now.Add(2*time.Hour), nil)
	if _, ok := s.Snapshot(now.Add(2*time.Hour), true).CCErrors["0x0100"]; ok {
		t.Error("stale PID counter must be forgotten")
	}
}

func TestPCRJitterWindow(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	now := time.Unix(1_800_000_000, 0)
	s.HandleLine("* pcrverify: PID 0x0101 (257), PCR jitter: 270,000 = 10,000 micro-seconds = 0 packets", now, nil)
	s.HandleLine("* pcrverify: PID 0x0101 (257), PCR jitter: -162,000 = 6,000 micro-seconds = 0 packets", now, nil)
	if snap := s.Snapshot(now, true); snap.PCRExceeded != 2 || snap.PCRMaxUS != 0 {
		t.Errorf("before interval end: exceeded=%v max=%v", snap.PCRExceeded, snap.PCRMaxUS)
	}
	s.HandleLine(`* analyze: AN:{"ts":{"bitrate":1},"pids":[],"services":[],"tables":[]}`, now, nil)
	if snap := s.Snapshot(now, true); snap.PCRMaxUS != 10000 {
		t.Errorf("max after interval = %v, want 10000", snap.PCRMaxUS)
	}
	s.HandleLine(`* analyze: AN:{"ts":{"bitrate":1},"pids":[],"services":[],"tables":[]}`, now, nil)
	if snap := s.Snapshot(now, true); snap.PCRMaxUS != 0 || snap.PCRExceeded != 2 {
		t.Errorf("next quiet interval: max=%v exceeded=%v", snap.PCRMaxUS, snap.PCRExceeded)
	}
}

func TestSCTEFromRealLog(t *testing.T) {
	f, err := os.Open("../tsp/testdata/splicemonitor.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Лог СТС: out (id 11261, 165 с) → in (11262) → out (11263, 75 с) …
	s := New("233.32.71.170:6000", "СТС", 5*time.Second)
	log := &memLog{}
	now := time.Unix(1_800_000_000, 0)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		now = now.Add(time.Second)
		s.HandleLine(sc.Text(), now, log)
	}

	snap := s.Snapshot(now, true).SCTE
	if !snap.Seen {
		t.Fatal("scte must be seen")
	}
	if snap.Events["out"] == 0 || snap.Events["in"] == 0 {
		t.Errorf("events = %v", snap.Events)
	}
	if snap.Commands["splice_insert"] == 0 {
		t.Errorf("commands = %v", snap.Commands)
	}
	if snap.Preroll["out"] < 1 || snap.Preroll["out"] > 15 {
		t.Errorf("out preroll = %v, want ~10s", snap.Preroll["out"])
	}
	scte := log.kinds(KindSCTE35)
	if len(scte) == 0 {
		t.Fatal("scte35 events must be logged")
	}
	var occurredOut bool
	for _, f := range scte {
		if f["command"] == "splice_null" {
			t.Fatal("splice_null must not be logged")
		}
		if f["event_type"] == "out" && f["progress"] == "occurred" {
			occurredOut = true
		}
	}
	if !occurredOut {
		t.Error("occurred out event must be logged")
	}
	var msgs []string
	for _, f := range scte {
		msgs = append(msgs, f["message"].(string))
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"SCTE-35: splice_insert out, блок 165 с, id 11261", "SCTE-35: начало рекламы (out), id 11261", "SCTE-35: анонс #1 — начало рекламы (out) через 9.5 с, id 11261"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}

func TestBreakActive(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	t0 := time.Unix(1_800_000_000, 0)
	insert := `* splicemonitor: SCTE:{"#name":"splice_information_table","#nodes":[{"#name":"metadata","pid":777},` +
		`{"#name":"splice_insert","out_of_network":true,"splice_event_id":1,"#nodes":[{"#name":"break_duration","auto_return":false,"duration":5400000}]}]}`
	out := `* splicemonitor: SCTE:{"#name":"event","event-id":1,"event-type":"out","progress":"occurred","count":3,"pre-roll-ms":3948}`
	in := `* splicemonitor: SCTE:{"#name":"event","event-id":2,"event-type":"in","progress":"occurred","count":3}`

	s.HandleLine(insert, t0, nil)
	s.HandleLine(out, t0, nil)
	if snap := s.Snapshot(t0.Add(30*time.Second), true).SCTE; snap.Preroll["out"] != 3.948 {
		t.Errorf("preroll from occurred event = %v, want 3.948", snap.Preroll["out"])
	}
	if snap := s.Snapshot(t0.Add(30*time.Second), true).SCTE; !snap.BreakActive || snap.BreakDuration != 60 {
		t.Errorf("during break: active=%v duration=%v", snap.BreakActive, snap.BreakDuration)
	}
	// Возврат не пришёл: блок считается законченным после длительности + запаса
	if snap := s.Snapshot(t0.Add(2*time.Minute), true).SCTE; snap.BreakActive {
		t.Error("break must end after announced duration + grace")
	}
	s.HandleLine(in, t0.Add(40*time.Second), nil)
	if snap := s.Snapshot(t0.Add(45*time.Second), true).SCTE; snap.BreakActive {
		t.Error("break must end on in")
	}
}

func report(bitrate int64, pids string) string {
	return `* analyze: AN:{"ts":{"bitrate":` + itoa(bitrate) + `},"pids":[` + pids + `],"services":[{"name":"СТС","provider":"","type-name":"Digital television service"}],"tables":[]}`
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

const (
	pidVideo    = `{"id":256,"service-count":1,"video":true,"description":"AVC video (1920x1080)","packets":{"discontinuities":0}}`
	pidVideoCC  = `{"id":256,"service-count":1,"video":true,"description":"AVC video (1920x1080)","packets":{"discontinuities":7}}`
	pidAudioRus = `{"id":257,"service-count":1,"audio":true,"language":"rus","description":"MPEG-1 Audio (rus)","packets":{"discontinuities":0}}`
	pidAudioEng = `{"id":257,"service-count":1,"audio":true,"language":"eng","description":"MPEG-1 Audio (eng)","packets":{"discontinuities":0}}`
	pidTeletext = `{"id":258,"service-count":1,"description":"Teletext (rus)","language":"rus","packets":{"discontinuities":0}}`
)

func TestCCBurstLogged(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	log := &memLog{}
	t0 := time.Unix(1_800_000_000, 0)
	s.HandleLine(report(1000, pidVideo+","+pidAudioRus), t0, log)
	s.HandleLine(report(1000, pidVideoCC+","+pidAudioRus), t0.Add(5*time.Second), log)
	s.HandleLine(report(1000, pidVideoCC+","+pidAudioRus), t0.Add(10*time.Second), log)
	s.HandleLine(report(1000, pidVideo+","+pidAudioRus), t0.Add(15*time.Second), log)

	cc := log.kinds(KindCCErrors)
	if len(cc) != 2 {
		t.Fatalf("cc events = %v, want start and end", cc)
	}
	if cc[0]["phase"] != "start" || cc[0]["pids"].(map[string]int64)["0x0100"] != 7 {
		t.Errorf("start = %v", cc[0])
	}
	if cc[0]["window_start"] != t0.Format(time.RFC3339Nano) {
		t.Errorf("window_start = %v, want start of the first bad interval %v", cc[0]["window_start"], t0)
	}
	if cc[1]["phase"] != "end" || cc[1]["total"] != int64(14) || cc[1]["duration_s"] != 15.0 {
		t.Errorf("end = %v", cc[1])
	}
	if cc[0]["message"] != "CC-ошибки начались: 0x0100×7" || cc[1]["message"] != "CC-ошибки закончились: 14 за 15 с — 0x0100×14" {
		t.Errorf("messages = %q / %q", cc[0]["message"], cc[1]["message"])
	}
}

func TestPIDChangeLoggedAfterConfirmation(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	log := &memLog{}
	t0 := time.Unix(1_800_000_000, 0)
	at := func(i int) time.Time { return t0.Add(time.Duration(i) * 5 * time.Second) }

	// Первый отчёт без разрешения видео, дальше с ним — это ещё не изменение, а исходный набор
	s.HandleLine(report(1000, `{"id":256,"service-count":1,"video":true,"description":"AVC video","packets":{}},`+pidAudioRus), at(-2), log)
	s.HandleLine(report(1000, pidVideo+","+pidAudioRus), at(-1), log)
	s.HandleLine(report(1000, pidVideo+","+pidAudioRus), at(0), log)
	// Одиночный сбой: аудио пропало на один отчёт — не логируем
	s.HandleLine(report(1000, pidVideo), at(1), log)
	s.HandleLine(report(1000, pidVideo+","+pidAudioRus), at(2), log)
	if got := log.kinds(KindPIDsChanged); len(got) != 0 {
		t.Fatalf("one-report glitch must not be logged: %v", got)
	}

	// Настоящее изменение: язык аудио rus → eng и новый телетекст
	s.HandleLine(report(1000, pidVideo+","+pidAudioEng+","+pidTeletext), at(3), log)
	s.HandleLine(report(1000, pidVideo+","+pidAudioEng+","+pidTeletext), at(4), log)
	got := log.kinds(KindPIDsChanged)
	if len(got) != 1 {
		t.Fatalf("pids_changed = %v, want 1", got)
	}
	added := got[0]["added"].([]map[string]string)
	changed := got[0]["changed"].([]map[string]any)
	if len(added) != 1 || added[0]["pid"] != "0x0102" || added[0]["codec"] != "teletext" {
		t.Errorf("added = %v", added)
	}
	if len(changed) != 1 || changed[0]["from"].(map[string]string)["language"] != "rus" || changed[0]["to"].(map[string]string)["language"] != "eng" {
		t.Errorf("changed = %v", changed)
	}
	if _, ok := got[0]["removed"]; ok {
		t.Errorf("nothing removed: %v", got[0])
	}
	if m := got[0]["message"]; m != "Состав PID изменился: добавлен 0x0102 teletext rus; 0x0101: mpeg1audio rus → mpeg1audio eng" {
		t.Errorf("message = %q", m)
	}
}

func TestStatusTransitionsLogged(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	log := &memLog{}
	t0 := s.created
	s.HandleLine(report(1000, pidVideo), t0.Add(time.Second), log)
	s.CheckStatus(t0.Add(2*time.Second), log)
	if len(log.kinds(KindStreamUp))+len(log.kinds(KindStreamDown)) != 0 {
		t.Fatal("first online state is not a transition")
	}
	s.CheckStatus(t0.Add(20*time.Second), log)
	down := log.kinds(KindStreamDown)
	if len(down) != 1 || down[0]["reason"] != "no data" {
		t.Fatalf("down = %v", down)
	}
	s.CheckStatus(t0.Add(21*time.Second), log)
	if len(log.kinds(KindStreamDown)) != 1 {
		t.Fatal("down must be logged once")
	}
	s.HandleLine(report(1000, pidVideo), t0.Add(30*time.Second), log)
	s.CheckStatus(t0.Add(30*time.Second), log)
	up := log.kinds(KindStreamUp)
	if len(up) != 1 || up[0]["down_duration_s"] != 10.0 || up[0]["message"] != "Поток вернулся, не было 10 с" {
		t.Fatalf("up = %v", up)
	}
}

func TestDeadAtStartLoggedOnce(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	log := &memLog{}
	s.CheckStatus(s.created.Add(5*time.Second), log)
	s.CheckStatus(s.created.Add(16*time.Second), log)
	s.CheckStatus(s.created.Add(30*time.Second), log)
	down := log.kinds(KindStreamDown)
	if len(down) != 1 || down[0]["reason"] != "no data since monitor start" {
		t.Fatalf("down = %v", down)
	}
}

func TestEmptyOrDeadReportNotAChange(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	log := &memLog{}
	t0 := time.Unix(1_800_000_000, 0)
	for i := 0; i < 3; i++ {
		s.HandleLine(report(1000, pidVideo), t0.Add(time.Duration(i)*5*time.Second), log)
	}
	for i := 3; i < 6; i++ {
		s.HandleLine(`* analyze: AN:{"ts":{"bitrate":0},"pids":[],"services":[],"tables":[]}`, t0.Add(time.Duration(i)*5*time.Second), log)
	}
	if n := len(log.kinds(KindPIDsChanged)) + len(log.kinds(KindServiceChanged)); n != 0 {
		t.Errorf("dead stream must not log changes, got %v", log.lines)
	}
}

func TestTSPExitRateLimited(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	log := &memLog{}
	t0 := time.Unix(1_800_000_000, 0)
	for i := 0; i < 130; i++ { // каждые 5 с почти 11 минут
		s.TSPExited(nil, t0.Add(time.Duration(i)*5*time.Second), log)
	}
	got := log.kinds(KindTSPExit)
	if len(got) != 2 || got[0]["exits"] != 1 || got[1]["exits"] != 120 {
		t.Errorf("tsp_exit = %v", got)
	}
}

func TestResolutionSticky(t *testing.T) {
	s := New("x", "x", 5*time.Second)
	log := &memLog{}
	t0 := time.Unix(1_800_000_000, 0)
	noRes := `{"id":256,"service-count":1,"video":true,"description":"AVC video","packets":{}}`
	hd := `{"id":256,"service-count":1,"video":true,"description":"AVC video (1280x720)","packets":{}}`
	seq := []string{pidVideo, pidVideo, noRes, noRes, noRes, pidVideo, noRes, hd, noRes, hd}
	for i, p := range seq {
		s.HandleLine(report(1000, p), t0.Add(time.Duration(i)*5*time.Second), log)
		if i == 4 {
			if snap := s.Snapshot(t0.Add(time.Duration(i)*5*time.Second), true); snap.PIDs[0].Resolution != "1920x1080" {
				t.Errorf("resolution must stay known, got %q", snap.PIDs[0].Resolution)
			}
		}
	}
	got := log.kinds(KindPIDsChanged)
	if len(got) != 1 {
		t.Fatalf("only the real 1080 → 720 change must be logged, got %v", got)
	}
	ch := got[0]["changed"].([]map[string]any)[0]
	if ch["from"].(map[string]string)["resolution"] != "1920x1080" || ch["to"].(map[string]string)["resolution"] != "1280x720" {
		t.Errorf("change = %v", ch)
	}
}
