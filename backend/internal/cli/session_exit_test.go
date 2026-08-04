package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// exitedServer accepts POST /api/v1/sessions/{id}/exited and records what the
// CLI sent, mirroring activityServer in hooks_test.go.
func exitedServer(t *testing.T, status int, respBody string) (*httptest.Server, *activityCapture) {
	t.Helper()
	capture := &activityCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/exited") {
			http.NotFound(w, r)
			return
		}
		capture.path = r.URL.Path
		capture.hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestSessionMarkExited_ReportsAgainstAO_SESSION_ID(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "ao-9")
	cfg := setConfigEnv(t)
	srv, capture := exitedServer(t, http.StatusOK, `{"ok":true,"sessionId":"ao-9"}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "mark-exited")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.hits != 1 {
		t.Fatalf("hits = %d, want 1", capture.hits)
	}
	if !strings.HasSuffix(capture.path, "/sessions/ao-9/exited") {
		t.Fatalf("path = %q, want suffix /sessions/ao-9/exited", capture.path)
	}
}

// A failed report must never fail the agent's own exit — mirrors ao hooks'
// own tolerance for an unreachable or erroring daemon.
func TestSessionMarkExited_DaemonErrorDoesNotFail(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "ao-9")
	cfg := setConfigEnv(t)
	srv, _ := exitedServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "mark-exited")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// No AO_SESSION_ID means this is not an AO-managed session: exit 0 without
// making any request, matching runHook's own guard in hooks.go.
func TestSessionMarkExited_NoSessionIDIsNoop(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := exitedServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "session", "mark-exited")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.hits != 0 {
		t.Fatalf("hits = %d, want 0", capture.hits)
	}
}
