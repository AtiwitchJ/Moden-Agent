package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
)

type fakeWorkboardService struct {
	cards          []domain.WorkCard
	createIn       workboardsvc.CreateInput
	updateID       string
	updateIn       workboardsvc.UpdateInput
	deleteID       string
	moveID         string
	moveStatus     domain.CardStatus
	movePos        int64
	nudgeID        string
	nudgeIn        workboardsvc.NudgeInput
	retargetID     string
	retargetIn     workboardsvc.RetargetInput
	splitID        string
	splitIn        workboardsvc.SplitInput
	recordEventID  string
	recordEventIn  workboardsvc.AgentEventInput
	handoffs       []workboardsvc.Handoff
	statusReason   string
	failure        workboardsvc.DispatchFailure
	directorStatus workboardsvc.DirectorStatus
}

func (f *fakeWorkboardService) Create(_ context.Context, in workboardsvc.CreateInput) (domain.WorkCard, error) {
	f.createIn = in
	if in.Agent == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_AGENT_REQUIRED", "Agent is required", nil)
	}
	return f.cards[0], nil
}

func (f *fakeWorkboardService) List(context.Context, string, string) ([]domain.WorkCard, error) {
	return f.cards, nil
}

func (f *fakeWorkboardService) ListAll(context.Context) ([]domain.WorkCard, error) {
	return f.cards, nil
}

func (f *fakeWorkboardService) ListRedo(context.Context, string) ([]domain.RedoCycle, error) {
	return nil, nil
}

func (f *fakeWorkboardService) LatestDispatchFailure(_ context.Context, cardID string) (workboardsvc.DispatchFailure, error) {
	if f.failure.Reason == "" {
		return workboardsvc.DispatchFailure{}, apierr.NotFound("WORK_CARD_DISPATCH_FAILURE_NOT_FOUND", "No dispatch failure found for this work card")
	}
	f.failure.CardID = cardID
	return f.failure, nil
}

func (f *fakeWorkboardService) DirectorStatus(_ context.Context, projectID string) (workboardsvc.DirectorStatus, error) {
	f.directorStatus.ProjectID = projectID
	return f.directorStatus, nil
}

func (f *fakeWorkboardService) Get(_ context.Context, id string) (domain.WorkCard, error) {
	for _, card := range f.cards {
		if card.ID == id {
			return card, nil
		}
	}
	return domain.WorkCard{}, apierr.NotFound("WORK_CARD_NOT_FOUND", "Unknown work card")
}

func (f *fakeWorkboardService) Update(_ context.Context, id string, in workboardsvc.UpdateInput) (domain.WorkCard, error) {
	f.updateID, f.updateIn = id, in
	return f.cards[0], nil
}

func (f *fakeWorkboardService) Delete(_ context.Context, id string) error {
	f.deleteID = id
	return nil
}

func (f *fakeWorkboardService) Move(_ context.Context, id string, status domain.CardStatus, position int64) (domain.WorkCard, error) {
	f.moveID, f.moveStatus, f.movePos = id, status, position
	return f.cards[0], nil
}

func (f *fakeWorkboardService) Nudge(_ context.Context, id string, in workboardsvc.NudgeInput) (domain.WorkCard, error) {
	f.nudgeID, f.nudgeIn = id, in
	return f.cards[0], nil
}

func (f *fakeWorkboardService) Retarget(_ context.Context, id string, in workboardsvc.RetargetInput) (domain.WorkCard, error) {
	f.retargetID, f.retargetIn = id, in
	return f.cards[0], nil
}

func (f *fakeWorkboardService) Split(_ context.Context, id string, in workboardsvc.SplitInput) (workboardsvc.SplitResult, error) {
	f.splitID, f.splitIn = id, in
	return workboardsvc.SplitResult{OldCard: f.cards[0], NewCard: f.cards[0]}, nil
}

func (f *fakeWorkboardService) RecordAgentEvent(_ context.Context, cardID string, in workboardsvc.AgentEventInput) (domain.WorkCard, error) {
	f.recordEventID, f.recordEventIn = cardID, in
	return f.cards[0], nil
}

func (f *fakeWorkboardService) Handoffs(context.Context, string) ([]workboardsvc.Handoff, error) {
	return f.handoffs, nil
}

func (f *fakeWorkboardService) StatusReason(context.Context, string) (string, error) {
	return f.statusReason, nil
}

func newWorkboardTestServer(t *testing.T, svc *fakeWorkboardService) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Workboard: svc}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

type fakeDispatchKicker struct {
	kicked []string
}

func (f *fakeDispatchKicker) Kick(projectID string) {
	f.kicked = append(f.kicked, projectID)
}

func newWorkboardDispatchTestServer(t *testing.T, svc *fakeWorkboardService, kicker *fakeDispatchKicker) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Workboard: svc, DispatchTrigger: kicker}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

type fakeWorkboardProjectManager struct {
	projectsvc.Manager
	project       projectsvc.Project
	lastSetConfig projectsvc.SetConfigInput
	lastUpdate    projectsvc.UpdateWorkboardAutonomousInput
	getCalls      int
	setCalls      int
}

func (f *fakeWorkboardProjectManager) Get(context.Context, domain.ProjectID) (projectsvc.GetResult, error) {
	f.getCalls++
	return projectsvc.GetResult{Status: "ok", Project: &f.project}, nil
}

func (f *fakeWorkboardProjectManager) SetConfig(_ context.Context, _ domain.ProjectID, in projectsvc.SetConfigInput) (projectsvc.Project, error) {
	f.setCalls++
	if err := in.Config.Validate(); err != nil {
		return projectsvc.Project{}, apierr.Invalid("INVALID_PROJECT_CONFIG", err.Error(), nil)
	}
	f.lastSetConfig = in
	f.project.Config = &in.Config
	return f.project, nil
}

func (f *fakeWorkboardProjectManager) UpdateWorkboardAutonomous(_ context.Context, _ domain.ProjectID, in projectsvc.UpdateWorkboardAutonomousInput) (projectsvc.Project, error) {
	f.lastUpdate = in
	if in.ShortTimeoutMinutes != nil && (*in.ShortTimeoutMinutes < domain.MinWorkboardAutonomousShortTimeoutMinutes || *in.ShortTimeoutMinutes > domain.MaxWorkboardAutonomousShortTimeoutMinutes) {
		return projectsvc.Project{}, apierr.Invalid("INVALID_PROJECT_CONFIG", "workboard.autonomous.shortTimeoutMinutes: must be between 1 and 1440", nil)
	}
	config := domain.ProjectConfig{}
	if f.project.Config != nil {
		config = *f.project.Config
	}
	autonomous := config.Workboard.Autonomous
	if autonomous == (domain.WorkboardAutonomousConfig{}) {
		autonomous = domain.DefaultWorkboardConfig().Autonomous
	}
	if in.Enabled != nil {
		autonomous.Enabled = *in.Enabled
	}
	if in.Mode != nil {
		autonomous.Mode = *in.Mode
	}
	if in.ShortTimeoutMinutes != nil {
		autonomous.ShortTimeoutMinutes = *in.ShortTimeoutMinutes
	}
	if in.Sticky != nil {
		autonomous.Sticky = *in.Sticky
	}
	config.Workboard.Autonomous = autonomous
	if err := config.Validate(); err != nil {
		return projectsvc.Project{}, apierr.Invalid("INVALID_PROJECT_CONFIG", err.Error(), nil)
	}
	f.project.Config = &config
	return f.project, nil
}

func newWorkboardConfigTestServer(t *testing.T, projects projectsvc.Manager) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Projects: projects}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateWorkCard_Validation(t *testing.T) {
	srv := newWorkboardTestServer(t, &fakeWorkboardService{})

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/projects/proj/workboard/cards", `{"title":"Card","notes":"Details","priority":"normal","labels":["api"],"targetPath":"/repo","agent":""}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "WORK_CARD_AGENT_REQUIRED")
}

func TestGetWorkCardIncludesHandoffs(t *testing.T) {
	svc := &fakeWorkboardService{
		cards: []domain.WorkCard{{ID: "card_1", Title: "Card"}},
		handoffs: []workboardsvc.Handoff{{
			Phase: "coding", Summary: "Implemented SCB page", ChangedFiles: []string{"src/app.ts"}, Checks: []string{"npm test: pass"}, Next: "Review validation",
		}},
	}
	srv := newWorkboardTestServer(t, svc)
	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/workboard/cards/card_1", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"handoffs":[{"phase":"coding"`) || !strings.Contains(string(body), "Review validation") {
		t.Fatalf("get card = status %d body %s, want handoff", status, body)
	}
}

func TestGetWorkCardIncludesStatusReason(t *testing.T) {
	svc := &fakeWorkboardService{
		cards:        []domain.WorkCard{{ID: "card_1", Title: "Card", Status: domain.CardStatusBlocked}},
		statusReason: "card is underspecified: no target file named",
	}
	srv := newWorkboardTestServer(t, svc)
	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/workboard/cards/card_1", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"statusReason":"card is underspecified: no target file named"`) {
		t.Fatalf("get card = status %d body %s, want statusReason", status, body)
	}
}

func TestGetWorkCardOmitsStatusReasonWhenEmpty(t *testing.T) {
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{ID: "card_1", Title: "Card"}}}
	srv := newWorkboardTestServer(t, svc)
	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/workboard/cards/card_1", "")
	if status != http.StatusOK || strings.Contains(string(body), "statusReason") {
		t.Fatalf("get card = status %d body %s, want no statusReason field", status, body)
	}
}

func TestMoveWorkCard_RequiresPosition(t *testing.T) {
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{ID: "card_1"}}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/workboard/cards/card_1/move", `{"status":"ready"}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "WORK_CARD_POSITION_REQUIRED")
}

func TestUpdateWorkCard_NullScheduledAtClearsSchedule(t *testing.T) {
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{ID: "card_1"}}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodPatch, "/api/v1/workboard/cards/card_1", `{"scheduledAt":null}`)
	if status != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%s", status, body)
	}
	if !svc.updateIn.ScheduledAt.Set || svc.updateIn.ScheduledAt.Value != nil {
		t.Fatalf("scheduledAt update = %+v, want explicit nil", svc.updateIn.ScheduledAt)
	}
}

func TestUpdateWorkboardAutonomous_PatchesOnlyAutonomousSettings(t *testing.T) {
	projects := &fakeWorkboardProjectManager{project: projectsvc.Project{
		ID: "proj", Config: &domain.ProjectConfig{
			Heartbeat: domain.HeartbeatConfig{Enabled: true, Interval: "30m"},
			Workboard: domain.WorkboardConfig{WIPLimit: 7},
		},
	}}
	srv := newWorkboardConfigTestServer(t, projects)

	body, status, _ := doRequest(t, srv, http.MethodPatch, "/api/v1/projects/proj/workboard/autonomous", `{"enabled":true,"mode":"short_timeout","shortTimeoutMinutes":5,"sticky":false}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	got := *projects.project.Config
	if !got.Workboard.Autonomous.Enabled || got.Workboard.Autonomous.Mode != domain.WorkboardAutonomousModeShortTimeout || got.Workboard.Autonomous.ShortTimeoutMinutes != 5 || got.Workboard.Autonomous.Sticky {
		t.Fatalf("autonomous config = %+v", got.Workboard.Autonomous)
	}
	if !got.Heartbeat.Enabled || got.Heartbeat.Interval != "30m" || got.Workboard.WIPLimit != 7 {
		t.Fatalf("unrelated config was overwritten: %+v", got)
	}
	if projects.getCalls != 0 || projects.setCalls != 0 {
		t.Fatalf("controller used whole-config update: Get=%d SetConfig=%d", projects.getCalls, projects.setCalls)
	}
}

func TestUpdateWorkboardAutonomous_RejectsOverflowingShortTimeout(t *testing.T) {
	projects := &fakeWorkboardProjectManager{project: projectsvc.Project{ID: "proj"}}
	srv := newWorkboardConfigTestServer(t, projects)

	body, status, _ := doRequest(t, srv, http.MethodPatch, "/api/v1/projects/proj/workboard/autonomous", `{"shortTimeoutMinutes":999999999}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_PROJECT_CONFIG")
}

func TestWorkboardAPI_CardCRUDAndMove(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{
		ID: "card_1", ProjectID: "proj", BoardID: "default", Title: "Card", Notes: "Details",
		Priority: domain.CardPriorityNormal, Labels: []string{"api"}, Status: domain.CardStatusTriage,
		TargetPath: "/repo", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/proj/workboard/cards", "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", status, body)
	}
	var list struct {
		Cards []struct {
			ID     string   `json:"id"`
			Labels []string `json:"labels"`
		} `json:"cards"`
	}
	mustJSON(t, body, &list)
	if len(list.Cards) != 1 || list.Cards[0].ID != "card_1" {
		t.Fatalf("list = %+v", list)
	}
	if list.Cards[0].Labels == nil {
		t.Fatal("list labels must encode as [] rather than null")
	}

	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/projects/proj/workboard/cards", `{"title":"Card","notes":"Details","priority":"normal","labels":["api"],"targetPath":"/repo","agent":"codex"}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", status, body)
	}
	if svc.createIn.ProjectID != "proj" || svc.createIn.BoardID != "" || svc.createIn.Agent != "codex" || svc.createIn.Status != "" {
		t.Fatalf("create input = %+v", svc.createIn)
	}

	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/workboard/cards/card_1", "")
	if status != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", status, body)
	}

	body, status, _ = doRequest(t, srv, http.MethodDelete, "/api/v1/workboard/cards/card_1", "")
	if status != http.StatusOK {
		t.Fatalf("delete status = %d, want 200; body=%s", status, body)
	}
	if svc.deleteID != "card_1" {
		t.Fatalf("delete id = %q, want card_1", svc.deleteID)
	}

	body, status, _ = doRequest(t, srv, http.MethodPatch, "/api/v1/workboard/cards/card_1", `{"priority":"high","position":4}`)
	if status != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%s", status, body)
	}
	if svc.updateID != "card_1" || svc.updateIn.Priority == nil || *svc.updateIn.Priority != domain.CardPriorityHigh || svc.updateIn.Position == nil || *svc.updateIn.Position != 4 {
		t.Fatalf("update = id=%q input=%+v", svc.updateID, svc.updateIn)
	}

	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/workboard/cards/card_1/move", `{"status":"ready","position":0}`)
	if status != http.StatusOK {
		t.Fatalf("move status = %d, want 200; body=%s", status, body)
	}
	if svc.moveID != "card_1" || svc.moveStatus != domain.CardStatusReady || svc.movePos != 0 {
		t.Fatalf("move = id=%q status=%q position=%d", svc.moveID, svc.moveStatus, svc.movePos)
	}
}

func TestWorkboardAPI_RunningCardActions(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{
		ID: "card_1", ProjectID: "proj", BoardID: "default", Title: "Card", Notes: "Details",
		Priority: domain.CardPriorityNormal, Labels: []string{"api"}, Status: domain.CardStatusRunning,
		TargetPath: "/repo", Agent: "codex", SessionID: "sess-1", GoalVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/workboard/cards/card_1/nudge", `{"message":"Keep going"}`)
	if status != http.StatusOK {
		t.Fatalf("nudge status = %d, want 200; body=%s", status, body)
	}
	if svc.nudgeID != "card_1" || svc.nudgeIn.Message != "Keep going" {
		t.Fatalf("nudge = id=%q input=%+v", svc.nudgeID, svc.nudgeIn)
	}

	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/workboard/cards/card_1/retarget", `{"title":"New goal"}`)
	if status != http.StatusOK {
		t.Fatalf("retarget status = %d, want 200; body=%s", status, body)
	}
	if svc.retargetID != "card_1" || svc.retargetIn.Title == nil || *svc.retargetIn.Title != "New goal" {
		t.Fatalf("retarget = id=%q input=%+v", svc.retargetID, svc.retargetIn)
	}

	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/workboard/cards/card_1/split", `{"title":"Split card","notes":"Follow-up","startImmediately":true}`)
	if status != http.StatusOK {
		t.Fatalf("split status = %d, want 200; body=%s", status, body)
	}
	if svc.splitID != "card_1" || svc.splitIn.Title != "Split card" || !svc.splitIn.StartImmediately {
		t.Fatalf("split = id=%q input=%+v", svc.splitID, svc.splitIn)
	}
}

func TestWorkboardAPI_NilServiceReturnsNotImplemented(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/proj/workboard/cards", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestDispatchWorkboardEndpoint_KicksTrigger(t *testing.T) {
	svc := &fakeWorkboardService{}
	kicker := &fakeDispatchKicker{}
	srv := newWorkboardDispatchTestServer(t, svc, kicker)

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/projects/proj/workboard/dispatch", "")
	if status != http.StatusAccepted {
		t.Fatalf("dispatch status = %d, want 202; body=%s", status, body)
	}
	if len(kicker.kicked) != 1 || kicker.kicked[0] != "proj" {
		t.Fatalf("kicked = %v, want [proj]", kicker.kicked)
	}
}

func TestDispatchFailureEndpoint_ReturnsSafeReason(t *testing.T) {
	svc := &fakeWorkboardService{failure: workboardsvc.DispatchFailure{
		Reason: "hermes_unavailable", AttemptedAt: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/workboard/cards/card_1/dispatch-failure", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	if svc.failure.CardID != "card_1" {
		t.Fatalf("card ID = %q, want card_1", svc.failure.CardID)
	}
	if !strings.Contains(string(body), `"reason":"hermes_unavailable"`) || strings.Contains(string(body), "raw daemon error") {
		t.Fatalf("body = %s", body)
	}
}
