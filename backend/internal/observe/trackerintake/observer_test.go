package trackerintake

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
)

func TestPollSpawnsWorkerForEligibleIssue(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			Path:          "/tmp/demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
				Enabled:  true,
				Assignee: "alice",
			}},
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Fix login",
		Body:      "The login form submits twice.",
		State:     domain.IssueOpen,
		URL:       "https://github.com/acme/demo/issues/12",
		Labels:    []string{"agent-ready"},
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn calls = %d, want 1", len(spawner.calls))
	}
	call := spawner.calls[0]
	if call.ProjectID != "demo" || call.Kind != domain.KindWorker {
		t.Fatalf("spawn config = %+v", call)
	}
	if call.IssueID != "github:acme/demo#12" {
		t.Fatalf("IssueID = %q, want canonical github id", call.IssueID)
	}
	if !strings.Contains(call.Prompt, "Fix login") || !strings.Contains(call.Prompt, "The login form submits twice.") {
		t.Fatalf("prompt missing issue context:\n%s", call.Prompt)
	}
	if len(tracker.filters) != 1 {
		t.Fatalf("tracker filters = %d, want 1", len(tracker.filters))
	}
	if got := tracker.filters[0]; got.State != domain.ListOpen || got.Assignee != "alice" || len(got.Labels) != 0 {
		t.Fatalf("tracker filter = %+v", got)
	}
}

func TestPollCreatesTriageCardForWorkboardProject(t *testing.T) {
	root := t.TempDir()
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			Path:          root,
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config: domain.ProjectConfig{
				Worker: domain.RoleOverride{Harness: domain.HarnessCodex},
				TrackerIntake: domain.TrackerIntakeConfig{
					Enabled:  true,
					Assignee: "alice",
				},
				Workboard: domain.WorkboardConfig{WIPLimit: 3},
			},
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Fix login",
		Body:      "The login form submits twice.",
		State:     domain.IssueOpen,
		URL:       "https://github.com/acme/demo/issues/12",
		Labels:    []string{"agent-ready"},
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}
	cards := &fakeCardCreator{}

	if err := New(singleResolver(tracker), store, spawner, Config{
		Logger:      discardLogger(),
		CardCreator: cards,
	}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0 for workboard intake", len(spawner.calls))
	}
	if len(cards.calls) != 1 {
		t.Fatalf("card create calls = %d, want 1", len(cards.calls))
	}
	call := cards.calls[0]
	if call.ProjectID != "demo" || call.Status != domain.CardStatusTriage || call.Agent != "codex" {
		t.Fatalf("create input = %+v", call)
	}
	if call.Title != "Fix login" {
		t.Fatalf("Title = %q, want issue title", call.Title)
	}
	if call.TargetPath != root {
		t.Fatalf("TargetPath = %q, want %q", call.TargetPath, root)
	}
	if !strings.Contains(call.Notes, "Fix login") || !strings.Contains(call.Notes, "The login form submits twice.") {
		t.Fatalf("notes missing issue context:\n%s", call.Notes)
	}
	if len(call.Labels) != 1 || call.Labels[0] != "agent-ready" {
		t.Fatalf("Labels = %#v", call.Labels)
	}
}

func TestPollSkipsExistingTriageCards(t *testing.T) {
	root := t.TempDir()
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			Path:          root,
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config: domain.ProjectConfig{
				Worker: domain.RoleOverride{Harness: domain.HarnessCodex},
				TrackerIntake: domain.TrackerIntakeConfig{
					Enabled:  true,
					Assignee: "alice",
				},
				Workboard: domain.WorkboardConfig{WIPLimit: 3},
			},
		}},
		cards: []domain.WorkCard{{
			ID:        "card-1",
			ProjectID: "demo",
			Notes:     BuildIssueCardNotes(domain.Issue{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"}, Title: "Already triaged"}),
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Already triaged",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	cards := &fakeCardCreator{}

	if err := New(singleResolver(tracker), store, &fakeSpawner{}, Config{
		Logger:      discardLogger(),
		CardCreator: cards,
	}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(cards.calls) != 0 {
		t.Fatalf("card create calls = %d, want 0", len(cards.calls))
	}
}

func TestWorkboardIntakeGate(t *testing.T) {
	t.Run("absent workboard keeps legacy spawn", func(t *testing.T) {
		var cfg domain.WorkboardConfig
		if cfg.IntakeEnabled() {
			t.Fatal("empty workboard config should not enable intake cards")
		}
	})
	t.Run("workboard section defaults intake on", func(t *testing.T) {
		cfg := domain.WorkboardConfig{WIPLimit: 2}
		if !cfg.IntakeEnabled() {
			t.Fatal("workboard section should default intake to triage cards")
		}
	})
	t.Run("explicit false opts out", func(t *testing.T) {
		off := false
		cfg := domain.WorkboardConfig{WIPLimit: 2, WorkboardIntake: &off}
		if cfg.IntakeEnabled() {
			t.Fatal("workboardIntake=false should keep legacy spawn")
		}
	})
}

func TestIntakeTargetPathUsesFirstWorkspaceRepo(t *testing.T) {
	root := t.TempDir()
	store := &fakeStore{
		workspaceRepos: []domain.WorkspaceRepoRecord{
			{RelativePath: domain.RootWorkspaceRepoName},
			{RelativePath: "child-app"},
		},
	}
	project := domain.ProjectRecord{
		ID:   "ws",
		Path: root,
		Kind: domain.ProjectKindWorkspace,
	}
	got, err := intakeTargetPath(context.Background(), store, project)
	if err != nil {
		t.Fatalf("intakeTargetPath() error = %v", err)
	}
	want := filepath.Join(root, "child-app")
	if got != want {
		t.Fatalf("target path = %q, want %q", got, want)
	}
}

func TestPollSkipsExistingIssueSessionsAfterRestart(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{ID: "demo-1", ProjectID: "demo", IssueID: "github:acme/demo#12"}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Already running",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0", len(spawner.calls))
	}
}

func TestPollSkipsSessionScanWhenIntakeDisabled(t *testing.T) {
	store := &fakeStore{
		projects:    []domain.ProjectRecord{{ID: "demo"}},
		sessionsErr: errors.New("session scan should not run"),
	}

	if err := New(singleResolver(&fakeTracker{}), store, &fakeSpawner{}, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v, want nil", err)
	}
}

func TestPollSkipsIneligibleAndInvalidProjects(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{
			{ID: "off", RepoOriginURL: "https://github.com/acme/off.git"},
			{ID: "broad", RepoOriginURL: "https://github.com/acme/broad.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true}}},
			{ID: "missing-origin", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
		},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/off#1"},
		Title: "ignored",
		State: domain.IssueOpen,
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(tracker.repos) != 0 {
		t.Fatalf("tracker was called for invalid/off projects: %+v", tracker.repos)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0", len(spawner.calls))
	}
}

func TestPollContinuesAfterTrackerAndSpawnFailures(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{
		{ID: "bad", RepoOriginURL: "https://github.com/acme/bad.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
		{ID: "good", RepoOriginURL: "https://github.com/acme/good.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
	}}
	tracker := &fakeTracker{
		failRepos: map[string]error{"acme/bad": errors.New("rate limited")},
		issuesByRepo: map[string][]domain.Issue{
			"acme/good": {
				{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/good#1"}, Title: "first", State: domain.IssueOpen, Assignees: []string{"alice"}},
				{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/good#2"}, Title: "second", State: domain.IssueOpen, Assignees: []string{"alice"}},
			},
		},
	}
	spawner := &fakeSpawner{failIssue: domain.IssueID("github:acme/good#1")}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("spawn attempts = %d, want 2", len(spawner.calls))
	}
	if spawner.calls[1].IssueID != "github:acme/good#2" {
		t.Fatalf("second spawn issue = %q", spawner.calls[1].IssueID)
	}
}

func TestPollBacksOffProjectAfterFailure(t *testing.T) {
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{failRepos: map[string]error{"acme/demo": errors.New("rate limited")}}
	observer := New(singleResolver(tracker), store, &fakeSpawner{}, Config{
		Clock:          func() time.Time { return now },
		FailureBackoff: time.Minute,
		Logger:         discardLogger(),
	})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}
	if len(tracker.repos) != 1 {
		t.Fatalf("tracker calls after first poll = %d, want 1", len(tracker.repos))
	}

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}
	if len(tracker.repos) != 1 {
		t.Fatalf("tracker calls during backoff = %d, want still 1", len(tracker.repos))
	}

	now = now.Add(time.Minute + time.Nanosecond)
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("third Poll() error = %v", err)
	}
	if len(tracker.repos) != 2 {
		t.Fatalf("tracker calls after backoff = %d, want 2", len(tracker.repos))
	}
}

func TestPollSkipsNonOpenIssueStates(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"}, Title: "already active", State: domain.IssueInProgress, Assignees: []string{"alice"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#2"}, Title: "ready", State: domain.IssueOpen, Assignees: []string{"alice"}},
	}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#2" {
		t.Fatalf("spawn calls = %+v, want only open issue #2", spawner.calls)
	}
}

func TestPollAppliesLocalEligibilityFilter(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"}, Title: "unassigned", State: domain.IssueOpen},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#2"}, Title: "wrong assignee", State: domain.IssueOpen, Assignees: []string{"bob"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#3"}, Title: "eligible", State: domain.IssueOpen, Labels: []string{"Agent-Ready"}, Assignees: []string{"Alice"}},
	}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#3" {
		t.Fatalf("spawn calls = %+v, want only eligible issue #3", spawner.calls)
	}
}

func TestIssueMatchesConfigAssigneeSpecialValues(t *testing.T) {
	assigned := domain.Issue{Assignees: []string{"alice"}}
	unassigned := domain.Issue{}
	if !issueMatchesConfig(assigned, domain.TrackerIntakeConfig{Assignee: "*"}) {
		t.Fatal("assigned issue should match assignee=*")
	}
	if issueMatchesConfig(unassigned, domain.TrackerIntakeConfig{Assignee: "*"}) {
		t.Fatal("unassigned issue should not match assignee=*")
	}
	if !issueMatchesConfig(unassigned, domain.TrackerIntakeConfig{Assignee: "none"}) {
		t.Fatal("unassigned issue should match assignee=none")
	}
	if issueMatchesConfig(assigned, domain.TrackerIntakeConfig{Assignee: "none"}) {
		t.Fatal("assigned issue should not match assignee=none")
	}
}

func TestBuildIssuePromptCapsLargeIssueBody(t *testing.T) {
	prompt := BuildIssuePrompt(domain.Issue{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#99"},
		Title: "Large issue",
		URL:   "https://github.com/acme/demo/issues/99",
		Body:  strings.Repeat("body ", 2000),
	})
	if len(prompt) > maxIntakePromptLen {
		t.Fatalf("prompt length = %d, want <= %d", len(prompt), maxIntakePromptLen)
	}
	if !strings.Contains(prompt, "Issue content truncated") {
		t.Fatalf("prompt missing truncation notice:\n%s", prompt)
	}
	if !strings.Contains(prompt, "https://github.com/acme/demo/issues/99") {
		t.Fatalf("prompt missing issue URL:\n%s", prompt)
	}
	if !strings.HasSuffix(prompt, intakePromptFooter) {
		t.Fatalf("prompt missing footer:\n%s", prompt)
	}
}

func TestTrackerRepoUsesConfiguredRepo(t *testing.T) {
	project := domain.ProjectRecord{
		ID:            "demo",
		RepoOriginURL: "https://github.com/wrong/repo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled:  true,
			Repo:     "acme/demo",
			Assignee: "alice",
		}},
	}
	repo, ok := trackerRepo(project, project.Config.TrackerIntake.WithDefaults())
	if !ok {
		t.Fatal("trackerRepo ok = false")
	}
	if repo.Native != "acme/demo" {
		t.Fatalf("repo.Native = %q, want acme/demo", repo.Native)
	}
}

func singleResolver(tracker ports.Tracker) TrackerResolver {
	return SingleTrackerResolver{Provider: domain.TrackerProviderGitHub, Adapter: tracker}
}

type fakeStore struct {
	projects       []domain.ProjectRecord
	sessions       []domain.SessionRecord
	sessionsErr    error
	cards          []domain.WorkCard
	cardsErr       error
	workspaceRepos []domain.WorkspaceRepoRecord
}

func (f *fakeStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return append([]domain.ProjectRecord(nil), f.projects...), nil
}

func (f *fakeStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return append([]domain.SessionRecord(nil), f.sessions...), f.sessionsErr
}

func (f *fakeStore) ListWorkCards(_ context.Context, projectID, _ string) ([]domain.WorkCard, error) {
	if f.cardsErr != nil {
		return nil, f.cardsErr
	}
	out := make([]domain.WorkCard, 0, len(f.cards))
	for _, card := range f.cards {
		if card.ProjectID == projectID {
			out = append(out, card)
		}
	}
	return out, nil
}

func (f *fakeStore) ListWorkspaceRepos(context.Context, string) ([]domain.WorkspaceRepoRecord, error) {
	return append([]domain.WorkspaceRepoRecord(nil), f.workspaceRepos...), nil
}

type fakeTracker struct {
	issues       []domain.Issue
	issuesByRepo map[string][]domain.Issue
	failRepos    map[string]error
	repos        []domain.TrackerRepo
	filters      []domain.ListFilter
}

func (f *fakeTracker) Get(context.Context, domain.TrackerID) (domain.Issue, error) {
	return domain.Issue{}, nil
}

func (f *fakeTracker) List(_ context.Context, repo domain.TrackerRepo, filter domain.ListFilter) ([]domain.Issue, error) {
	f.repos = append(f.repos, repo)
	f.filters = append(f.filters, filter)
	if err := f.failRepos[repo.Native]; err != nil {
		return nil, err
	}
	if f.issuesByRepo != nil {
		return append([]domain.Issue(nil), f.issuesByRepo[repo.Native]...), nil
	}
	return append([]domain.Issue(nil), f.issues...), nil
}

func (f *fakeTracker) Preflight(context.Context) error { return nil }

type fakeSpawner struct {
	calls     []ports.SpawnConfig
	failIssue domain.IssueID
}

func (f *fakeSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	f.calls = append(f.calls, cfg)
	if cfg.IssueID == f.failIssue {
		return domain.Session{}, errors.New("spawn failed")
	}
	return domain.Session{SessionRecord: domain.SessionRecord{ID: domain.SessionID(string(cfg.ProjectID) + "-1"), ProjectID: cfg.ProjectID, IssueID: cfg.IssueID, Kind: cfg.Kind}}, nil
}

type fakeCardCreator struct {
	calls []workboardsvc.CreateInput
	err   error
}

func (f *fakeCardCreator) Create(_ context.Context, in workboardsvc.CreateInput) (domain.WorkCard, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return domain.WorkCard{}, f.err
	}
	return domain.WorkCard{ID: "card-1", ProjectID: in.ProjectID, Title: in.Title, Notes: in.Notes, Status: in.Status}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
