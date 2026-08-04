package controllers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

type fakeExitReporter struct {
	gotID domain.SessionID
	calls int
	err   error
}

func (f *fakeExitReporter) MarkTerminated(_ context.Context, id domain.SessionID) error {
	f.calls++
	f.gotID = id
	return f.err
}

func newExitedTestServer(t *testing.T, rep *fakeExitReporter) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := httpd.APIDeps{}
	if rep != nil {
		deps.ExitReporter = rep
	}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, deps, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSessionsAPI_ExitedMarksTerminatedWithoutTeardown(t *testing.T) {
	rep := &fakeExitReporter{}
	srv := newExitedTestServer(t, rep)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions/ao-1/exited", "")
	if status != http.StatusOK {
		t.Fatalf("exited = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		OK        bool   `json:"ok"`
		SessionID string `json:"sessionId"`
	}
	mustJSON(t, body, &resp)
	if !resp.OK || resp.SessionID != "ao-1" {
		t.Fatalf("exited response = %#v", resp)
	}
	if rep.calls != 1 || rep.gotID != "ao-1" {
		t.Fatalf("reporter calls=%d id=%q", rep.calls, rep.gotID)
	}
}

func TestSessionsAPI_ExitedMissingSessionIs404(t *testing.T) {
	srv := newExitedTestServer(t, &fakeExitReporter{err: ports.ErrSessionNotFound})

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions/missing/exited", "")
	assertErrorCode(t, body, status, http.StatusNotFound, "SESSION_NOT_FOUND")
}

func TestSessionsAPI_ExitedReporterErrorIs500(t *testing.T) {
	srv := newExitedTestServer(t, &fakeExitReporter{err: errors.New("boom")})

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions/ao-1/exited", "")
	assertErrorCode(t, body, status, http.StatusInternalServerError, "INTERNAL_ERROR")
}

func TestSessionsAPI_ExitedWithoutReporterIs501(t *testing.T) {
	srv := newExitedTestServer(t, nil)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions/ao-1/exited", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}
