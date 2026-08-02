package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkboardGetAndStatusUseDaemonAPI(t *testing.T) {
	cfg := setConfigEnv(t)
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		payload, _ := io.ReadAll(r.Body)
		body = string(payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"card-1","projectId":"p1","title":"Ship","labels":["billing"],"codingAgent":"claude-code","goalVersion":2,"status":"`+map[bool]string{true: "done", false: "running"}[r.Method == http.MethodPatch]+`"}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "get", "card-1", "--json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, `"codingAgent": "claude-code"`) || !strings.Contains(out, `"goalVersion": 2`) {
		t.Fatalf("get output missing full card fields: %s", out)
	}
	if method != http.MethodGet || path != "/api/v1/workboard/cards/card-1" {
		t.Fatalf("get request = %s %s", method, path)
	}
	if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "status", "card-1", "done"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if method != http.MethodPatch || path != "/api/v1/workboard/cards/card-1" {
		t.Fatalf("status request = %s %s", method, path)
	}
	var request workCardStatusRequest
	if err := json.Unmarshal([]byte(body), &request); err != nil || request.Status != "done" {
		t.Fatalf("status body = %q err=%v", body, err)
	}
}

func TestWorkboardStatusRejectsUnknownStatus(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "workboard", "status", "card-1", "nope")
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("error=%v exit=%d, want usage error", err, ExitCode(err))
	}
}
