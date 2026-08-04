package workboard

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func newAgentEventService(status domain.CardStatus) (*Service, *actionsStoreFake) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Fix", Notes: "Details",
		Status: status, Agent: "codex", SessionID: "sess-1",
	}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}}
	svc := NewWithDeps(Deps{
		Store: store, Clock: func() time.Time { return now }, NewID: func() string { return "evt-1" },
	})
	return svc, store
}

// fakeAgentReporter records ReportVerdict calls so tests can assert
// RecordAgentEvent wires the right phase/verdict through to the orchestrator,
// without depending on the real commander/orchestrator package.
type fakeAgentReporter struct {
	calls []reportedVerdict
	err   error
}

type reportedVerdict struct {
	CardID, Phase, Verdict string
}

func (f *fakeAgentReporter) ReportVerdict(_ context.Context, cardID, phase, verdict string) error {
	f.calls = append(f.calls, reportedVerdict{CardID: cardID, Phase: phase, Verdict: verdict})
	return f.err
}

func newAgentEventServiceWithReporter(status domain.CardStatus) (*Service, *actionsStoreFake, *fakeAgentReporter) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Fix", Notes: "Details",
		Status: status, Agent: "codex", SessionID: "sess-1",
	}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}}
	reporter := &fakeAgentReporter{}
	svc := NewWithDeps(Deps{
		Store: store, Reporter: reporter, Clock: func() time.Time { return now }, NewID: func() string { return "evt-1" },
	})
	return svc, store, reporter
}

func TestRecordAgentEventReportsApprovedCodingHandoffAsCompletion(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_handoff",
		Payload: `{"phase":"coding","summary":"Updated login form"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	want := []reportedVerdict{{CardID: "card-1", Phase: "coding", Verdict: "approved"}}
	if !reflect.DeepEqual(reporter.calls, want) {
		t.Fatalf("reporter calls = %+v, want %+v", reporter.calls, want)
	}
}

func TestRecordAgentEventDoesNotReportAReviewOrTestingHandoff(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusReview)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_handoff",
		Payload: `{"phase":"review","summary":"Looks good"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if len(reporter.calls) != 0 {
		t.Fatalf("reporter calls = %+v, want none — only a coding handoff is a completion signal", reporter.calls)
	}
}

func TestRecordAgentEventReportsVerdictWithPhaseFromCardStatus(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusReview)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_verdict",
		Payload: `{"verdict":"changes_requested"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	want := []reportedVerdict{{CardID: "card-1", Phase: "review", Verdict: "changes_requested"}}
	if !reflect.DeepEqual(reporter.calls, want) {
		t.Fatalf("reporter calls = %+v, want %+v", reporter.calls, want)
	}
}

func TestRecordAgentEventSkipsVerdictReportWhenCardHasNoActivePhase(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusDone)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_verdict",
		Payload: `{"verdict":"approved"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if len(reporter.calls) != 0 {
		t.Fatalf("reporter calls = %+v, want none for a card with no active phase", reporter.calls)
	}
}

func TestRecordAgentEventReportsTestResultPassOnZeroExit(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusTesting)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "test_result",
		Payload: `{"command":"npm test","exit":0,"output":"ok"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	want := []reportedVerdict{{CardID: "card-1", Phase: "testing", Verdict: "pass"}}
	if !reflect.DeepEqual(reporter.calls, want) {
		t.Fatalf("reporter calls = %+v, want %+v", reporter.calls, want)
	}
}

func TestRecordAgentEventReportsTestResultFailOnNonZeroExit(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusTesting)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "test_result",
		Payload: `{"command":"npm test","exit":1,"output":"fail"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	want := []reportedVerdict{{CardID: "card-1", Phase: "testing", Verdict: "fail"}}
	if !reflect.DeepEqual(reporter.calls, want) {
		t.Fatalf("reporter calls = %+v, want %+v", reporter.calls, want)
	}
}

func TestRecordAgentEventReportsAgentFailedWithReasonAsVerdict(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_failed",
		Payload: `{"reason":"timeout"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	want := []reportedVerdict{{CardID: "card-1", Phase: "coding", Verdict: "timeout"}}
	if !reflect.DeepEqual(reporter.calls, want) {
		t.Fatalf("reporter calls = %+v, want %+v", reporter.calls, want)
	}
}

func TestRecordAgentEventSurvivesReporterError(t *testing.T) {
	svc, _, reporter := newAgentEventServiceWithReporter(domain.CardStatusReview)
	reporter.err = errors.New("orchestrator boom")
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_verdict",
		Payload: `{"verdict":"approved"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v, want a reporter failure to stay best-effort", err)
	}
}

func TestRecordAgentEventAppendsVerdict(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	card, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_verdict",
		Payload: `{"verdict":"approved"}`,
	})
	if err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if card.Status != domain.CardStatusRunning {
		t.Fatalf("status = %s, want running (unchanged)", card.Status)
	}
	if len(store.events) != 1 || store.events[0].Kind != "agent_verdict" {
		t.Fatalf("events = %+v, want one agent_verdict", store.events)
	}
}

func TestRecordAgentEventAppendsHandoff(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	_, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_handoff",
		Payload: `{"phase":"coding","summary":"Updated login form","changedFiles":["app.ts"],"checks":["npm test: pass"]}`,
	})
	if err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if len(store.events) != 1 || store.events[0].Kind != "agent_handoff" {
		t.Fatalf("events = %+v, want one agent_handoff", store.events)
	}
}

func TestHandoffsReturnsDurablePhaseContext(t *testing.T) {
	svc, _ := newAgentEventService(domain.CardStatusReview)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind: "agent_handoff", Payload: `{"phase":"coding","summary":"Updated login form","changedFiles":["app.ts"],"checks":["npm test: pass"],"next":"Review validation"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	handoffs, err := svc.Handoffs(context.Background(), "card-1")
	if err != nil {
		t.Fatalf("Handoffs: %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].Summary != "Updated login form" || handoffs[0].Next != "Review validation" {
		t.Fatalf("handoffs = %#v", handoffs)
	}
}

func TestRecordAgentEventRejectsInvalidHandoff(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	_, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind: "agent_handoff", Payload: `{"phase":"deployment","summary":"Done"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "phase") {
		t.Fatalf("RecordAgentEvent error = %v, want invalid handoff", err)
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %+v, want none", store.events)
	}
}

func TestRecordAgentEventAppliesTransition(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	card, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"review","position":0}`,
	})
	if err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if card.Status != domain.CardStatusReview {
		t.Fatalf("status = %s, want review", card.Status)
	}
	if got := store.cards["card-1"].Status; got != domain.CardStatusReview {
		t.Fatalf("persisted status = %s, want review", got)
	}
	if len(store.events) != 1 || store.events[0].Kind != "agent_transition" {
		t.Fatalf("events = %+v, want one agent_transition", store.events)
	}
}

func TestStatusReasonReturnsTheReasonForTheCurrentStatus(t *testing.T) {
	svc, _ := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind: "agent_transition", Payload: `{"status":"blocked","reason":"card is underspecified: no target file named"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	reason, err := svc.StatusReason(context.Background(), "card-1")
	if err != nil {
		t.Fatalf("StatusReason: %v", err)
	}
	if reason != "card is underspecified: no target file named" {
		t.Fatalf("reason = %q, want the recorded blocked reason", reason)
	}
}

func TestStatusReasonIsEmptyWhenNoTransitionEventExplainsTheCurrentStatus(t *testing.T) {
	svc, _ := newAgentEventService(domain.CardStatusRunning)
	reason, err := svc.StatusReason(context.Background(), "card-1")
	if err != nil {
		t.Fatalf("StatusReason: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty for a card with no transition history", reason)
	}
}

func TestStatusReasonIgnoresAStaleReasonFromAnEarlierStatus(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind: "agent_transition", Payload: `{"status":"review","reason":"coding done"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	// A later, non-transition write must not clear the still-current reason.
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind: "agent_verdict", Payload: `{"verdict":"approved"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	reason, err := svc.StatusReason(context.Background(), "card-1")
	if err != nil {
		t.Fatalf("StatusReason: %v", err)
	}
	if reason != "coding done" {
		t.Fatalf("reason = %q, want the review transition's reason to still apply", reason)
	}

	// Move the card again directly in the store without a matching transition
	// event (e.g. a legacy or non-agent path) — the old reason must not leak
	// forward onto an unrelated status.
	card := store.cards["card-1"]
	card.Status = domain.CardStatusTesting
	store.cards["card-1"] = card
	reason, err = svc.StatusReason(context.Background(), "card-1")
	if err != nil {
		t.Fatalf("StatusReason: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty once the status no longer matches the last transition event", reason)
	}
}

func TestRecordAgentEventRejectsIllegalTransition(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	_, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"done"}`,
	})
	if err == nil {
		t.Fatal("RecordAgentEvent: want error for running->done, got nil")
	}
	if !strings.Contains(err.Error(), "running") {
		t.Fatalf("error = %v, want it to name the rejected transition", err)
	}
	if got := store.cards["card-1"].Status; got != domain.CardStatusRunning {
		t.Fatalf("status = %s, want the card left in running", got)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %+v, want the attempt still recorded", store.events)
	}
}

func TestRecordAgentEventRejectsUnknownKind(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind: "please_delete_everything",
	}); err == nil {
		t.Fatal("RecordAgentEvent: want error for unknown kind, got nil")
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %+v, want nothing recorded for a rejected kind", store.events)
	}
}

func TestRecordAgentEventRejectsNonJSONPayload(t *testing.T) {
	svc, _ := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_finding",
		Payload: "not json",
	}); err == nil {
		t.Fatal("RecordAgentEvent: want error for non-JSON payload, got nil")
	}
}

func TestRecordAgentEventTransitionClearsActiveSession(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"review"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if len(store.deletedActiveSessions) != 1 || store.deletedActiveSessions[0] != "card-1" {
		t.Fatalf("deletedActiveSessions = %v, want [card-1]", store.deletedActiveSessions)
	}
}

func TestRecordAgentEventNonTransitionDoesNotClearActiveSession(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_verdict",
		Payload: `{"verdict":"approved"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if len(store.deletedActiveSessions) != 0 {
		t.Fatalf("deletedActiveSessions = %v, want none for a non-transition event", store.deletedActiveSessions)
	}
}

func TestRecordAgentEventRejectedTransitionDoesNotClearActiveSession(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"done"}`,
	}); err == nil {
		t.Fatal("RecordAgentEvent: want error for running->done, got nil")
	}
	if len(store.deletedActiveSessions) != 0 {
		t.Fatalf("deletedActiveSessions = %v, want none for a rejected transition", store.deletedActiveSessions)
	}
}
