package workboard

import (
	"context"
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
