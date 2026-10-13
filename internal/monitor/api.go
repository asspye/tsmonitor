package monitor

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/otcnet/tsmonitor/internal/control"
)

// API включения/выключения потоков без правки config.yaml.
//
//	GET   /api/v1/streams[?enabled=true|false]  список потоков
//	GET   /api/v1/streams/{url}                 один поток
//	PATCH /api/v1/streams/{url}                 {"enabled": false, "comment": "…"}
//
// {url} — url потока из конфига (233.198.134.1:3333); для HLS — с %2F вместо «/».
// PATCH требует Authorization: Bearer <TSMONITOR_API_TOKEN>.

// maxBody — предел тела PATCH
const maxBody = 4 << 10

// StreamInfo — поток в ответах API
type StreamInfo struct {
	URL           string     `json:"url"`
	Description   string     `json:"description"`
	Enabled       bool       `json:"enabled"`
	Online        bool       `json:"online"`
	DisabledSince *time.Time `json:"disabled_since,omitempty"`
	Comment       string     `json:"comment,omitempty"`
}

type streamPatch struct {
	Enabled *bool   `json:"enabled"`
	Comment *string `json:"comment"`
}

var errNotFound = errors.New("stream not found")

func (o *Orchestrator) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/streams", o.handleList)
	mux.HandleFunc("GET /api/v1/streams/{url}", o.handleGet)
	mux.HandleFunc("PATCH /api/v1/streams/{url}", o.handlePatch)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, http.StatusNotFound, "not_found", "unknown endpoint "+r.Method+" "+r.URL.Path)
	})
}

func (o *Orchestrator) info(ch *channel, now time.Time) StreamInfo {
	st := ch.state
	in := StreamInfo{URL: st.URL, Description: st.Description, Enabled: true, Online: st.Online(now)}
	if e, ok := o.control.Disabled(st.URL); ok {
		since := e.Since
		in.Enabled, in.DisabledSince, in.Comment = false, &since, e.Comment
	}
	return in
}

func (o *Orchestrator) handleList(w http.ResponseWriter, r *http.Request) {
	var filter *bool
	switch v := r.URL.Query().Get("enabled"); v {
	case "":
	case "true", "false":
		b := v == "true"
		filter = &b
	default:
		apiError(w, http.StatusBadRequest, "invalid_parameter", "enabled must be true or false")
		return
	}
	now := time.Now()
	list := make([]StreamInfo, 0, len(o.channels))
	for _, ch := range o.channels {
		in := o.info(ch, now)
		if filter == nil || in.Enabled == *filter {
			list = append(list, in)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": list, "count": len(list)})
}

func (o *Orchestrator) handleGet(w http.ResponseWriter, r *http.Request) {
	ch, ok := o.byURL[r.PathValue("url")]
	if !ok {
		apiError(w, http.StatusNotFound, "not_found", errNotFound.Error())
		return
	}
	writeJSON(w, http.StatusOK, o.info(ch, time.Now()))
}

func (o *Orchestrator) handlePatch(w http.ResponseWriter, r *http.Request) {
	if !o.authorized(w, r) {
		return
	}
	ch, ok := o.byURL[r.PathValue("url")]
	if !ok {
		apiError(w, http.StatusNotFound, "not_found", errNotFound.Error())
		return
	}

	var p streamPatch
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "body must be JSON like {\"enabled\": false, \"comment\": \"…\"}: "+err.Error())
		return
	}
	if p.Enabled == nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "field enabled is required")
		return
	}
	comment := ""
	if p.Comment != nil {
		comment = strings.TrimSpace(*p.Comment)
	}

	if err := o.setEnabled(ch, *p.Enabled, comment); err != nil {
		apiError(w, http.StatusInternalServerError, "state_write_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, o.info(ch, time.Now()))
}

// setEnabled включает или выключает поток. Сначала сохраняется файл состояния:
// если запись не удалась, поток остаётся как был. Повторное выключение меняет
// только комментарий.
func (o *Orchestrator) setEnabled(ch *channel, enabled bool, comment string) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	st := ch.state

	if !enabled {
		e := control.Entry{Since: time.Now(), Comment: comment}
		if old, ok := o.control.Disabled(st.URL); ok {
			e.Since = old.Since
		}
		if err := o.control.SetDisabled(st.URL, e); err != nil {
			return err
		}
		st.Disable(comment, o.events)
		o.stopStream(ch)
		return nil
	}

	if err := o.control.SetEnabled(st.URL); err != nil {
		return err
	}
	if !st.Enabled() {
		st.Enable(time.Now(), o.events)
	}
	o.startStream(ch)
	return nil
}

// authorized проверяет Bearer-токен; без настроенного токена изменения запрещены
func (o *Orchestrator) authorized(w http.ResponseWriter, r *http.Request) bool {
	token := o.config.APIToken
	if token == "" {
		apiError(w, http.StatusForbidden, "read_only", "changes are disabled: TSMONITOR_API_TOKEN is not set")
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tsmonitor"`)
		apiError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Printf("❌ API response: %v\n", err)
	}
}

func apiError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
