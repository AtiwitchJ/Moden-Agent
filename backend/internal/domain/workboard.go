package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type CardStatus string

const (
	CardStatusTriage    CardStatus = "triage"
	CardStatusBacklog   CardStatus = "backlog"
	CardStatusTodo      CardStatus = "todo"
	CardStatusScheduled CardStatus = "scheduled"
	CardStatusReady     CardStatus = "ready"
	CardStatusRunning   CardStatus = "running"
	CardStatusReview    CardStatus = "review"
	CardStatusBlocked   CardStatus = "blocked"
	CardStatusDone      CardStatus = "done"
)

func ParseCardStatus(s string) (CardStatus, error) {
	switch CardStatus(s) {
	case CardStatusTriage, CardStatusBacklog, CardStatusTodo, CardStatusScheduled,
		CardStatusReady, CardStatusRunning, CardStatusReview, CardStatusBlocked, CardStatusDone:
		return CardStatus(s), nil
	default:
		return "", fmt.Errorf("invalid card status %q", s)
	}
}

// ValidateCardStatus reports whether s is a known card status.
func ValidateCardStatus(s string) error {
	_, err := ParseCardStatus(s)
	return err
}

type CardPriority string

const (
	CardPriorityLow    CardPriority = "low"
	CardPriorityNormal CardPriority = "normal"
	CardPriorityHigh   CardPriority = "high"
	CardPriorityUrgent CardPriority = "urgent"
)

func ParseCardPriority(s string) (CardPriority, error) {
	switch CardPriority(s) {
	case CardPriorityLow, CardPriorityNormal, CardPriorityHigh, CardPriorityUrgent:
		return CardPriority(s), nil
	default:
		return "", fmt.Errorf("invalid card priority %q", s)
	}
}

// PriorityRank higher = claimed sooner.
func (p CardPriority) Rank() int {
	switch p {
	case CardPriorityUrgent:
		return 4
	case CardPriorityHigh:
		return 3
	case CardPriorityNormal:
		return 2
	case CardPriorityLow:
		return 1
	default:
		return 0
	}
}

type WorkCard struct {
	ID                 string
	ProjectID          string
	BoardID            string
	Title              string
	Notes              string
	Priority           CardPriority
	Labels             []string
	Status             CardStatus
	ScheduledAt        *time.Time
	ReadyAt            *time.Time
	Position           int64
	TargetPath         string
	RepoName           string
	Agent              string
	SessionID          string
	WaitingForInput    bool
	PausedRetarget     bool
	GoalVersion        int
	SupersededByCardID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// WorkCardEvent is an append-only audit fact associated with a work card.
// Payload is JSON owned by the event producer.
type WorkCardEvent struct {
	ID        string
	CardID    string
	ProjectID string
	Kind      string
	Payload   string
	CreatedAt time.Time
}

type WorkboardAutonomousConfig struct {
	// Enabled and Sticky are always serialized so clients can round-trip
	// false without confusing omitempty + UI defaults.
	Enabled             bool   `json:"enabled"`
	Mode                string `json:"mode,omitempty" enum:"skip_timeout,short_timeout"`
	ShortTimeoutMinutes int    `json:"shortTimeoutMinutes,omitempty" minimum:"1" maximum:"1440"`
	Sticky              bool   `json:"sticky"`
}

const (
	WorkboardAutonomousModeSkipTimeout  = "skip_timeout"
	WorkboardAutonomousModeShortTimeout = "short_timeout"

	MinWorkboardAutonomousShortTimeoutMinutes = 1
	MaxWorkboardAutonomousShortTimeoutMinutes = 24 * 60
)

// Validate rejects autonomous settings that could produce an unsafe answer
// timeout. The all-zero config remains valid so projects without workboard
// settings continue to store no workboard override.
func (c WorkboardAutonomousConfig) Validate() error {
	if c == (WorkboardAutonomousConfig{}) {
		return nil
	}
	if c.Mode != WorkboardAutonomousModeSkipTimeout && c.Mode != WorkboardAutonomousModeShortTimeout {
		return fmt.Errorf("workboard.autonomous.mode: must be %q or %q", WorkboardAutonomousModeSkipTimeout, WorkboardAutonomousModeShortTimeout)
	}
	if c.ShortTimeoutMinutes != 0 && (c.ShortTimeoutMinutes < MinWorkboardAutonomousShortTimeoutMinutes || c.ShortTimeoutMinutes > MaxWorkboardAutonomousShortTimeoutMinutes) {
		return fmt.Errorf("workboard.autonomous.shortTimeoutMinutes: must be between %d and %d", MinWorkboardAutonomousShortTimeoutMinutes, MaxWorkboardAutonomousShortTimeoutMinutes)
	}
	if c.Mode == WorkboardAutonomousModeShortTimeout && c.ShortTimeoutMinutes == 0 {
		return fmt.Errorf("workboard.autonomous.shortTimeoutMinutes: must be between %d and %d", MinWorkboardAutonomousShortTimeoutMinutes, MaxWorkboardAutonomousShortTimeoutMinutes)
	}
	return nil
}

// WorkboardAutonomousPatch is a sparse update to autonomous workboard
// settings. Nil fields leave the persisted value unchanged.
type WorkboardAutonomousPatch struct {
	Enabled             *bool
	Mode                *string
	ShortTimeoutMinutes *int
	Sticky              *bool
}

// Validate checks values explicitly supplied in a sparse patch.
func (p WorkboardAutonomousPatch) Validate() error {
	if p.Mode != nil && *p.Mode != WorkboardAutonomousModeSkipTimeout && *p.Mode != WorkboardAutonomousModeShortTimeout {
		return fmt.Errorf("workboard.autonomous.mode: must be %q or %q", WorkboardAutonomousModeSkipTimeout, WorkboardAutonomousModeShortTimeout)
	}
	if p.ShortTimeoutMinutes != nil && (*p.ShortTimeoutMinutes < MinWorkboardAutonomousShortTimeoutMinutes || *p.ShortTimeoutMinutes > MaxWorkboardAutonomousShortTimeoutMinutes) {
		return fmt.Errorf("workboard.autonomous.shortTimeoutMinutes: must be between %d and %d", MinWorkboardAutonomousShortTimeoutMinutes, MaxWorkboardAutonomousShortTimeoutMinutes)
	}
	return nil
}

// ApplyTo applies p to c without changing fields that p omits.
func (p WorkboardAutonomousPatch) ApplyTo(c WorkboardAutonomousConfig) WorkboardAutonomousConfig {
	if p.Enabled != nil {
		c.Enabled = *p.Enabled
	}
	if p.Mode != nil {
		c.Mode = *p.Mode
	}
	if p.ShortTimeoutMinutes != nil {
		c.ShortTimeoutMinutes = *p.ShortTimeoutMinutes
	}
	if p.Sticky != nil {
		c.Sticky = *p.Sticky
	}
	return c
}

// WithDefaults fills values required to make a sparse autonomous config
// actionable while preserving skip-timeout's intentionally absent minutes.
func (c WorkboardAutonomousConfig) WithDefaults() WorkboardAutonomousConfig {
	defaults := DefaultWorkboardConfig().Autonomous
	if c == (WorkboardAutonomousConfig{}) {
		return defaults
	}
	if c.Mode == "" {
		c.Mode = defaults.Mode
	}
	if c.Mode == WorkboardAutonomousModeShortTimeout && c.ShortTimeoutMinutes == 0 {
		c.ShortTimeoutMinutes = defaults.ShortTimeoutMinutes
	}
	return c
}

type WorkboardConfig struct {
	WIPLimit             int                       `json:"wipLimit,omitempty"`
	FallbackAgents       []string                  `json:"fallbackAgents,omitempty"`
	LimitCooldownMinutes int                       `json:"limitCooldownMinutes,omitempty"`
	AnswerTimeoutMinutes int                       `json:"answerTimeoutMinutes,omitempty"`
	Autonomous           WorkboardAutonomousConfig `json:"autonomous,omitempty"`
	AnswerDenylist       []string                  `json:"answerDenylist,omitempty"`
	// WorkboardIntake controls whether tracker intake creates triage cards
	// instead of spawning worker sessions. When the workboard section is
	// present, intake defaults to triage unless explicitly set to false.
	WorkboardIntake *bool `json:"workboardIntake,omitempty"`
}

// IntakeEnabled reports whether tracker intake should create triage work cards
// for this project. A missing workboard section keeps legacy direct spawn.
func (c WorkboardConfig) IntakeEnabled() bool {
	if !c.isConfigured() {
		return false
	}
	if c.WorkboardIntake != nil {
		return *c.WorkboardIntake
	}
	return true
}

func (c WorkboardConfig) isConfigured() bool {
	if c.WIPLimit != 0 || c.LimitCooldownMinutes != 0 || c.AnswerTimeoutMinutes != 0 {
		return true
	}
	if len(c.FallbackAgents) > 0 || len(c.AnswerDenylist) > 0 {
		return true
	}
	if c.Autonomous != (WorkboardAutonomousConfig{}) {
		return true
	}
	return c.WorkboardIntake != nil
}

func DefaultWorkboardConfig() WorkboardConfig {
	return WorkboardConfig{
		WIPLimit:             3,
		LimitCooldownMinutes: 60,
		AnswerTimeoutMinutes: 10,
		Autonomous: WorkboardAutonomousConfig{
			Mode:                WorkboardAutonomousModeSkipTimeout,
			ShortTimeoutMinutes: 2,
			Sticky:              true,
		},
		AnswerDenylist: []string{"force_push", "delete_repo", "exfil_secret"},
	}
}

// TargetPathAllowed reports whether absPath is under one of the registered repo roots.
func TargetPathAllowed(absPath string, repoRoots []string) bool {
	clean := filepath.Clean(absPath)
	for _, root := range repoRoots {
		r := filepath.Clean(root)
		if clean == r || strings.HasPrefix(clean, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ValidateTargetPathUnderRepos reports an error when absPath is outside all registered repo roots.
func ValidateTargetPathUnderRepos(absPath string, repoRoots []string) error {
	if TargetPathAllowed(absPath, repoRoots) {
		return nil
	}
	return fmt.Errorf("target path %q is not under a registered repository", absPath)
}
