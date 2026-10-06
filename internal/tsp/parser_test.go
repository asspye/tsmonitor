package tsp

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

type wantPID struct {
	pid, typ, codec, lang, res string
}

func TestParseAnalyzeRealStreams(t *testing.T) {
	tests := []struct {
		file     string
		bitrate  int64
		service  string
		provider string
		svcType  string
		pids     map[int]wantPID
	}{
		{
			file: "analyze-233.32.71.45.json", bitrate: 18500396,
			service: "No Name", provider: "", svcType: "SD",
			pids: map[int]wantPID{
				200: {"0x00C8", "video", "h264", "", "1920x1080"},
				201: {"0x00C9", "audio", "mpeg1audio", "rus", ""},
				250: {"0x00FA", "data", "teletext", "und", ""},
			},
		},
		{
			file:    "analyze-hevc-subtitles.json",
			service: "Fashion 4K", provider: "Universal", svcType: "SD", // "HEVC digital television service": как и раньше, без "HD" → SD
			pids: map[int]wantPID{
				0x01F4: {"0x01F4", "video", "hevc", "", "3840x2160"},
				0x01F5: {"0x01F5", "audio", "aac", "eng", ""},
				0x021F: {"0x021F", "other", "scte_35_splice_info", "", ""},
				0x0384: {"0x0384", "data", "dvb_subtitle", "rus", ""},
			},
		},
		{
			file:    "analyze-ac3-teletext.json",
			service: "KAMEDI_HD", provider: "Unknown", svcType: "SD",
			pids: map[int]wantPID{
				0x0102: {"0x0102", "audio", "ac3", "", ""}, // stream_type 0x06, раньше было "private"
				0x0103: {"0x0103", "data", "teletext", "rus", ""},
				0x0104: {"0x0104", "other", "scte_35_splice_info", "", ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			r, err := ParseAnalyze(readFile(t, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if tt.bitrate != 0 && r.TS.Bitrate != tt.bitrate {
				t.Errorf("bitrate = %d, want %d", r.TS.Bitrate, tt.bitrate)
			}
			if len(r.Services) != 1 {
				t.Fatalf("services = %d, want 1", len(r.Services))
			}
			sv := r.Services[0]
			if sv.Name != tt.service || CleanProvider(sv.Provider) != tt.provider || ServiceType(sv.TypeName) != tt.svcType {
				t.Errorf("service = %q/%q/%s, want %q/%q/%s", sv.Name, CleanProvider(sv.Provider), ServiceType(sv.TypeName), tt.service, tt.provider, tt.svcType)
			}
			found := 0
			for i := range r.PIDs {
				p := &r.PIDs[i]
				w, ok := tt.pids[p.ID]
				if !ok {
					continue
				}
				found++
				if !p.IsElementary() {
					t.Errorf("PID %s must be elementary", w.pid)
				}
				got := wantPID{w.pid, PIDType(p), Codec(p.Description), p.Language, Resolution(p.Description)}
				if got != w {
					t.Errorf("PID %s = %+v, want %+v (description %q)", w.pid, got, w, p.Description)
				}
			}
			if found != len(tt.pids) {
				t.Errorf("found %d of %d expected PIDs", found, len(tt.pids))
			}
			for i := range r.PIDs {
				p := &r.PIDs[i]
				if (p.Global || p.PMT || p.ID == StuffingPID) && p.IsElementary() {
					t.Errorf("PID %d (%s) must not be elementary", p.ID, p.Description)
				}
			}
		})
	}
}

func TestParseAnalyzeTables(t *testing.T) {
	r, err := ParseAnalyze(readFile(t, "analyze-233.32.71.45.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, tb := range r.Tables {
		if name, ok := TableNames[tb.TID]; ok {
			got[name] = tb.RepetitionMS
		}
	}
	for _, name := range []string{"PAT", "PMT", "SDT", "NIT", "CAT"} {
		if got[name] <= 0 {
			t.Errorf("table %s repetition = %d, want > 0", name, got[name])
		}
	}
}

func TestClassifyAndParseLines(t *testing.T) {
	iat := "* iat: IAT: 10,749 microseconds (std.dev: 15197, min: 11, max: 33010), source: kernel, pkt/dgram: 5 (min: 1, max: 7)"
	kind, payload := Classify(iat)
	if kind != LineIAT {
		t.Fatalf("iat kind = %v", kind)
	}
	r, err := ParseIAT(payload)
	if err != nil {
		t.Fatal(err)
	}
	if *r != (IATReport{MeanUS: 10749, StdDevUS: 15197, MinUS: 11, MaxUS: 33010}) {
		t.Errorf("iat = %+v", *r)
	}

	pcr := "* pcrverify: PID 0x0101 (257), PCR jitter: -208,359 = 7,717 micro-seconds = 0 packets + 0 bytes + 0 bits (kernel time)"
	kind, payload = Classify(pcr)
	if kind != LinePCRJitter {
		t.Fatalf("pcr kind = %v", kind)
	}
	j, err := ParsePCRJitter(payload)
	if err != nil {
		t.Fatal(err)
	}
	if j.PID != 257 || j.JitterUS != 7717 {
		t.Errorf("pcr = %+v", *j)
	}

	if k, p := Classify(`* analyze: AN:{ "ts": {} }`); k != LineAnalyze || p != `{ "ts": {} }` {
		t.Errorf("analyze classify = %v %q", k, p)
	}
	if k, _ := Classify("* tsp: user interrupt, terminating..."); k != LineOther {
		t.Errorf("other classify = %v", k)
	}
}

func TestParseSCTERealLog(t *testing.T) {
	f, err := os.Open("testdata/splicemonitor.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	commands := map[string]int{}
	occurred := map[string]int{}
	var outDurations []float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		kind, payload := Classify(sc.Text())
		if kind != LineSCTE {
			continue
		}
		m, err := ParseSCTE(payload)
		if err != nil {
			t.Fatalf("%v: %s", err, payload)
		}
		switch {
		case m.IsTable:
			commands[m.Command]++
			if m.PID == 0 {
				t.Errorf("table without pid: %s", payload)
			}
			if m.Insert != nil && m.Insert.OutOfNetwork {
				outDurations = append(outDurations, m.Insert.BreakDuration)
			}
		case m.IsEvent:
			if m.Event.Occurred() {
				occurred[m.Event.Type]++
			}
		}
	}
	if commands["splice_insert"] == 0 || commands["splice_null"] == 0 {
		t.Errorf("commands = %v", commands)
	}
	if occurred["out"] == 0 || occurred["in"] == 0 {
		t.Errorf("occurred = %v", occurred)
	}
	if len(outDurations) == 0 || outDurations[0] != 165 {
		t.Errorf("first out break duration = %v, want 165s", outDurations)
	}
}

func TestBuildArgs(t *testing.T) {
	udp := BuildArgs(Options{
		LocalAddress: "172.22.2.154", URL: "233.32.71.45:5000",
		Interval: 5 * time.Second, ReceiveBuffer: 8388608, PCRJitterMax: 5 * time.Millisecond, SCTE35: true,
	})
	want := "-I ip --local-address 172.22.2.154 --buffer-size 8388608 233.32.71.45:5000 " +
		"-P iat --interval 5 -P pcrverify --input-synchronous --jitter-max 5000 " +
		"-P splicemonitor --all-commands --json-line=SCTE: -P analyze --interval 5 --json-line=AN: -O drop"
	if got := strings.Join(udp, " "); got != want {
		t.Errorf("udp args:\n got  %s\n want %s", got, want)
	}

	hls := strings.Join(BuildArgs(Options{URL: "/mnt/tmpvideo/hls/360p/index.m3u8", Interval: 5 * time.Second}), " ")
	if !strings.HasPrefix(hls, "-I hls --live /mnt/tmpvideo/hls/360p/index.m3u8 -P analyze") {
		t.Errorf("hls args: %s", hls)
	}
	if strings.Contains(hls, "iat") || strings.Contains(hls, "pcrverify") {
		t.Errorf("hls must not use iat/pcrverify: %s", hls)
	}
}

// MPTS 233.198.134.150: PMT сервиса Radio Belarus на PID 1000 = видео B24
func TestPMTOnVideoPID(t *testing.T) {
	r, err := ParseAnalyze(readFile(t, "analyze-pmt-pid-collision.json"))
	if err != nil {
		t.Fatal(err)
	}
	var video, pmt bool
	for i := range r.PIDs {
		p := &r.PIDs[i]
		switch p.ID {
		case 1000:
			video = p.IsElementary() && PIDType(p) == "video"
		case 1024:
			pmt = p.IsElementary()
		}
	}
	if !video {
		t.Error("video PID 1000 flagged as PMT must stay elementary")
	}
	if pmt {
		t.Error("pure PMT PID 1024 must not be elementary")
	}
}
