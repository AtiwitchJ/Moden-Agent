package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cardCapture records the method, path, and body of a received request.
type cardCapture struct {
	method string
	path   string
	body   []byte
}

// cardServer returns an httptest server that captures all requests and responds
// with {}. For GET /workboard/cards/{id} it returns a realistic card so
// ValidateWorkflowTransition can evaluate real transitions.
func cardServer(t *testing.T) (*httptest.Server, *[]cardCapture) {
	t.Helper()
	var calls []cardCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		calls = append(calls, cardCapture{method: r.Method, path: r.URL.Path, body: payload})
		w.Header().Set("Content-Type", "application/json")
		// Return a card with status "running" so running→review is a valid transition.
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/workboard/cards/") {
			_, _ = w.Write([]byte(`{"id":"card-1","projectId":"proj","title":"Fix","status":"running"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// cardServerWithStatus responds to every request with the given status and body.
func cardServerWithStatus(t *testing.T, status int, respBody string) (*httptest.Server, *[]cardCapture) {
	t.Helper()
	var calls []cardCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		calls = append(calls, cardCapture{method: r.Method, path: r.URL.Path, body: payload})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

func TestCardShow_HappyPath(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "show", "card-1")
	if err != nil {
		t.Fatalf("show: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "Fix") || !strings.Contains(out, "running") {
		t.Fatalf("show output missing expected fields: %s", out)
	}
}

func TestCardShow_JSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "show", "card-1", "--json")
	if err != nil {
		t.Fatalf("show --json: %v\nstderr=%s", err, errOut)
	}
	var got cardShowResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json: %v\nout=%s", err, out)
	}
	if got.ID != "card-1" || got.Title != "Fix" || got.Status != "running" {
		t.Fatalf("card json = %#v", got)
	}
}

func TestCardShow_NotYetImplemented(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusNotFound, `{"error":"not_found"}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "show", "card-1")
	if err == nil {
		t.Fatal("expected error for not-yet-implemented daemon route")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("error should mention not-yet-implemented: %v", err)
	}
}

// Note: cobra.ExactArgs(1) with no args produces a plain error (not usageError),
// so ExitCode is 1. The command runs correctly when args are provided.
func TestCardShow_MissingArg(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "show")
	if err == nil {
		t.Fatal("expected missing arg error")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
}

// ---------------------------------------------------------------------------
// transition
// ---------------------------------------------------------------------------

func TestCardTransition_HappyPath(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "transition", "card-1", "--to", "review", "--reason", "code review done")
	if err != nil {
		t.Fatalf("transition: %v\nstderr=%s", err, errOut)
	}
	if len(*calls) < 2 {
		t.Fatalf("expected at least 2 HTTP calls (card fetch + event), got %d", len(*calls))
	}
	// Find the event POST (skip telemetry).
	var eventCall *cardCapture
	for i := range *calls {
		if (*calls)[i].path == "/api/v1/workboard/cards/card-1/events" {
			eventCall = &(*calls)[i]
			break
		}
	}
	if eventCall == nil {
		t.Fatalf("expected POST /api/v1/workboard/cards/card-1/events; got calls: %v", *calls)
	}
	if eventCall.method != http.MethodPost {
		t.Fatalf("event call method = %s, want POST", eventCall.method)
	}
	var req cardEventRequest
	if err := json.Unmarshal(eventCall.body, &req); err != nil {
		t.Fatalf("decode event request: %v", err)
	}
	if req.Kind != "agent_transition" {
		t.Fatalf("event kind = %q, want agent_transition", req.Kind)
	}
	if !strings.Contains(out, "transitioned") {
		t.Fatalf("output missing transitioned confirmation: %s", out)
	}
}

// running→done is not a valid transition; ValidateWorkflowTransition returns usageError → exit 2.
func TestCardTransition_InvalidTransition(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "transition", "card-1", "--to", "done", "--reason", "want to close")
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)", ExitCode(err))
	}
}

// Missing --to: cobra's required flag enforcement produces a plain error → exit 1.
func TestCardTransition_MissingToFlag(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "transition", "card-1", "--reason", "why")
	if err == nil {
		t.Fatal("expected error for missing --to flag")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
}

// Missing --reason: cobra's required flag enforcement produces a plain error → exit 1.
func TestCardTransition_MissingReasonFlag(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "transition", "card-1", "--to", "review")
	if err == nil {
		t.Fatal("expected error for missing --reason flag")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
}

func TestCardTransition_DaemonError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusInternalServerError, `{"error":"internal","code":"SOMETHING_FAILED","message":"server error"}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "transition", "card-1", "--to", "review", "--reason", "testing")
	if err == nil {
		t.Fatal("expected error for daemon failure")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
	if !strings.Contains(err.Error(), "SOMETHING_FAILED") && !strings.Contains(errOut, "SOMETHING_FAILED") {
		t.Fatalf("error should surface daemon code: %v\nstderr=%s", err, errOut)
	}
}

// ---------------------------------------------------------------------------
// set-verdict
// ---------------------------------------------------------------------------

func TestCardSetVerdict_HappyPath(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-verdict", "card-1", "--verdict", "approved")
	if err != nil {
		t.Fatalf("set-verdict: %v\nstderr=%s", err, errOut)
	}
	// Skip telemetry invocation call.
	var eventCall *cardCapture
	for i := range *calls {
		if (*calls)[i].path == "/api/v1/workboard/cards/card-1/events" {
			eventCall = &(*calls)[i]
			break
		}
	}
	if eventCall == nil {
		t.Fatalf("expected POST /api/v1/workboard/cards/card-1/events; got calls: %v", *calls)
	}
	var req cardEventRequest
	if err := json.Unmarshal(eventCall.body, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Kind != "agent_verdict" {
		t.Fatalf("kind = %q, want agent_verdict", req.Kind)
	}
	if !strings.Contains(out, "approved") {
		t.Fatalf("output should mention verdict: %s", out)
	}
}

// Invalid verdict returns usageError → exit 2.
func TestCardSetVerdict_InvalidVerdict(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-verdict", "card-1", "--verdict", "bad_verdict")
	if err == nil {
		t.Fatal("expected error for invalid verdict")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2", ExitCode(err))
	}
	if !strings.Contains(err.Error(), "bad_verdict") {
		t.Fatalf("error should mention invalid verdict: %v", err)
	}
}

// Missing --verdict: cobra required flag enforcement → exit 1.
func TestCardSetVerdict_MissingFlag(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-verdict", "card-1")
	if err == nil {
		t.Fatal("expected error for missing --verdict flag")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
}

// ---------------------------------------------------------------------------
// set-finding
// ---------------------------------------------------------------------------

func TestCardSetFinding_HappyPath(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-finding", "card-1", "--severity", "high", "--title", "memory leak", "--details", "found in auth handler")
	if err != nil {
		t.Fatalf("set-finding: %v\nstderr=%s", err, errOut)
	}
	var eventCall *cardCapture
	for i := range *calls {
		if (*calls)[i].path == "/api/v1/workboard/cards/card-1/events" {
			eventCall = &(*calls)[i]
			break
		}
	}
	if eventCall == nil {
		t.Fatalf("expected POST /api/v1/workboard/cards/card-1/events; got calls: %v", *calls)
	}
	var req cardEventRequest
	if err := json.Unmarshal(eventCall.body, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Kind != "agent_finding" {
		t.Fatalf("kind = %q, want agent_finding", req.Kind)
	}
	if !strings.Contains(out, "finding recorded") {
		t.Fatalf("output should confirm finding: %s", out)
	}
}

// Invalid severity returns usageError → exit 2.
func TestCardSetFinding_InvalidSeverity(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-finding", "card-1", "--severity", "medium", "--title", "x", "--details", "y")
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2", ExitCode(err))
	}
	if !strings.Contains(err.Error(), "medium") {
		t.Fatalf("error should mention invalid severity: %v", err)
	}
}

// Missing required flags: cobra required flag enforcement → exit 1.
func TestCardSetFinding_MissingRequiredFlags(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-finding", "card-1")
	if err == nil {
		t.Fatal("expected error for missing required flags")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
}

// ---------------------------------------------------------------------------
// set-test-result
// ---------------------------------------------------------------------------

func TestCardSetTestResult_HappyPath(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-test-result", "card-1", "--command", "npm test", "--exit", "0")
	if err != nil {
		t.Fatalf("set-test-result: %v\nstderr=%s", err, errOut)
	}
	var eventCall *cardCapture
	for i := range *calls {
		if (*calls)[i].path == "/api/v1/workboard/cards/card-1/events" {
			eventCall = &(*calls)[i]
			break
		}
	}
	if eventCall == nil {
		t.Fatalf("expected POST /api/v1/workboard/cards/card-1/events; got calls: %v", *calls)
	}
	var req cardEventRequest
	if err := json.Unmarshal(eventCall.body, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Kind != "test_result" {
		t.Fatalf("kind = %q, want test_result", req.Kind)
	}
	if !strings.Contains(out, "test result recorded") {
		t.Fatalf("output should confirm test result: %s", out)
	}
}

// Missing --command: cobra required flag enforcement → exit 1.
func TestCardSetTestResult_MissingCommand(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "set-test-result", "card-1", "--exit", "1")
	if err == nil {
		t.Fatal("expected error for missing --command flag")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
}

// ---------------------------------------------------------------------------
// handoff
// ---------------------------------------------------------------------------

func TestCardHandoff_RecordsContextForNextPhase(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "handoff", "card-1", "--phase", "coding", "--summary", "Updated SCB page", "--changed", "src/app.ts", "--check", "npm test: pass", "--next", "Review validation")
	if err != nil {
		t.Fatalf("handoff: %v\nstderr=%s", err, errOut)
	}
	var eventCall *cardCapture
	for i := range *calls {
		if (*calls)[i].path == "/api/v1/workboard/cards/card-1/events" {
			eventCall = &(*calls)[i]
			break
		}
	}
	if eventCall == nil {
		t.Fatalf("expected handoff event; calls=%v", *calls)
	}
	var req cardEventRequest
	if err := json.Unmarshal(eventCall.body, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Kind != "agent_handoff" || !strings.Contains(req.Payload, "Updated SCB page") || !strings.Contains(req.Payload, "src/app.ts") {
		t.Fatalf("handoff request = %#v", req)
	}
	if !strings.Contains(out, "handoff recorded") {
		t.Fatalf("output = %s", out)
	}
}

// ---------------------------------------------------------------------------
// fail-attempt
// ---------------------------------------------------------------------------

func TestCardFailAttempt_HappyPath(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := cardServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "fail-attempt", "card-1", "--reason", "timeout")
	if err != nil {
		t.Fatalf("fail-attempt: %v\nstderr=%s", err, errOut)
	}
	var eventCall *cardCapture
	for i := range *calls {
		if (*calls)[i].path == "/api/v1/workboard/cards/card-1/events" {
			eventCall = &(*calls)[i]
			break
		}
	}
	if eventCall == nil {
		t.Fatalf("expected POST /api/v1/workboard/cards/card-1/events; got calls: %v", *calls)
	}
	var req cardEventRequest
	if err := json.Unmarshal(eventCall.body, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Kind != "agent_failed" {
		t.Fatalf("kind = %q, want agent_failed", req.Kind)
	}
	if !strings.Contains(out, "attempt failure recorded") {
		t.Fatalf("output should confirm failure: %s", out)
	}
}

// Invalid reason returns usageError → exit 2.
func TestCardFailAttempt_InvalidReason(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "fail-attempt", "card-1", "--reason", "bad_reason")
	if err == nil {
		t.Fatal("expected error for invalid reason")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2", ExitCode(err))
	}
	if !strings.Contains(err.Error(), "bad_reason") {
		t.Fatalf("error should mention invalid reason: %v", err)
	}
}

// Missing --reason: cobra required flag enforcement → exit 1.
func TestCardFailAttempt_MissingFlag(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := cardServerWithStatus(t, http.StatusOK, `{}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "workboard", "card", "fail-attempt", "card-1")
	if err == nil {
		t.Fatal("expected error for missing --reason flag")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCode(err))
	}
}
