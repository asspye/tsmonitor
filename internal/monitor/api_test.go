package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otcnet/tsmonitor/internal/config"
)

const (
	mcast = "233.198.134.1:3333"
	hls   = "http://192.168.1.155/hls/ch1/index.m3u8"
	token = "secret"
)

// fakeTSP кладёт в PATH «tsp», который просто ждёт сигнала
func fakeTSP(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tsp"), []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newTestOrchestrator(t *testing.T, stateFile string) (*Orchestrator, http.Handler) {
	t.Helper()
	cfg := &config.Config{
		Interface: "127.0.0.1",
		Interval:  time.Second,
		EventLog:  filepath.Join(t.TempDir(), "events.log"),
		StateFile: stateFile,
		APIToken:  token,
		Streams: []config.Stream{
			{URL: mcast, Description: "Channel 1"},
			{URL: hls, Description: "HLS 1"},
		},
	}
	o, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.ctx = ctx
	for _, ch := range o.channels {
		ch.mu.Lock()
		if ch.state.Enabled() {
			o.startStream(ch)
		}
		ch.mu.Unlock()
	}
	t.Cleanup(func() {
		cancel()
		o.wg.Wait()
		o.events.Close()
	})
	mux := http.NewServeMux()
	o.registerAPI(mux)
	return o, mux
}

func do(t *testing.T, h http.Handler, method, path, auth, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("%s %s: bad JSON %q", method, path, rec.Body.String())
	}
	return rec.Code, res
}

func running(o *Orchestrator, u string) bool {
	ch := o.byURL[u]
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.cancel != nil
}

func TestDisableEnable(t *testing.T) {
	fakeTSP(t)
	stateFile := filepath.Join(t.TempDir(), "state.json")
	o, h := newTestOrchestrator(t, stateFile)
	path := "/api/v1/streams/" + mcast

	if !running(o, mcast) {
		t.Fatal("tsp not started")
	}

	code, res := do(t, h, "PATCH", path, token, `{"enabled": false, "comment": "профилактика"}`)
	if code != http.StatusOK || res["enabled"] != false || res["comment"] != "профилактика" || res["disabled_since"] == nil {
		t.Fatalf("disable: %d %v", code, res)
	}
	if running(o, mcast) {
		t.Fatal("tsp still running after disable")
	}
	if !running(o, hls) {
		t.Fatal("other stream stopped")
	}

	code, res = do(t, h, "GET", "/api/v1/streams?enabled=false", "", "")
	if code != http.StatusOK || res["count"] != 1.0 {
		t.Fatalf("list disabled: %d %v", code, res)
	}

	// Выключение переживает перезапуск
	o2, h2 := newTestOrchestrator(t, stateFile)
	if running(o2, mcast) || o2.byURL[mcast].state.Enabled() {
		t.Fatal("disabled stream started after restart")
	}
	code, res = do(t, h2, "GET", path, "", "")
	if code != http.StatusOK || res["enabled"] != false || res["comment"] != "профилактика" {
		t.Fatalf("after restart: %d %v", code, res)
	}

	code, res = do(t, h2, "PATCH", path, token, `{"enabled": true}`)
	if code != http.StatusOK || res["enabled"] != true || res["comment"] != nil {
		t.Fatalf("enable: %d %v", code, res)
	}
	if !running(o2, mcast) {
		t.Fatal("tsp not restarted after enable")
	}

	events, _ := os.ReadFile(o2.config.EventLog)
	if !strings.Contains(string(events), `"kind":"stream_enabled"`) {
		t.Fatalf("no stream_enabled event: %s", events)
	}
}

func TestHLSURLInPath(t *testing.T) {
	fakeTSP(t)
	_, h := newTestOrchestrator(t, "")
	code, res := do(t, h, "GET", "/api/v1/streams/"+url.PathEscape(hls), "", "")
	if code != http.StatusOK || res["url"] != hls {
		t.Fatalf("hls get: %d %v", code, res)
	}
}

func TestAPIErrors(t *testing.T) {
	fakeTSP(t)
	o, h := newTestOrchestrator(t, "")
	path := "/api/v1/streams/" + mcast

	cases := []struct {
		name, method, path, auth, body string
		code                           int
		errCode                        string
	}{
		{"no token", "PATCH", path, "", `{"enabled": false}`, 401, "unauthorized"},
		{"bad token", "PATCH", path, "nope", `{"enabled": false}`, 401, "unauthorized"},
		{"unknown stream", "PATCH", "/api/v1/streams/1.2.3.4:5", token, `{"enabled": false}`, 404, "not_found"},
		{"no enabled", "PATCH", path, token, `{"comment": "x"}`, 400, "invalid_body"},
		{"unknown field", "PATCH", path, token, `{"enabled": false, "foo": 1}`, 400, "invalid_body"},
		{"bad filter", "GET", "/api/v1/streams?enabled=maybe", "", "", 400, "invalid_parameter"},
		{"unknown endpoint", "GET", "/api/v1/nope", "", "", 404, "not_found"},
	}
	for _, c := range cases {
		code, res := do(t, h, c.method, c.path, c.auth, c.body)
		e, _ := res["error"].(map[string]any)
		if code != c.code || e["code"] != c.errCode {
			t.Errorf("%s: %d %v", c.name, code, res)
		}
	}
	if !running(o, mcast) {
		t.Fatal("rejected requests changed the stream")
	}

	o.config.APIToken = ""
	code, res := do(t, h, "PATCH", path, "", `{"enabled": false}`)
	if e, _ := res["error"].(map[string]any); code != 403 || e["code"] != "read_only" {
		t.Fatalf("read-only: %d %v", code, res)
	}
}
