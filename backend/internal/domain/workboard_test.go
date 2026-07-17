package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestParseCardStatus(t *testing.T) {
	got, err := domain.ParseCardStatus("ready")
	if err != nil || got != domain.CardStatusReady {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := domain.ParseCardStatus("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkboardConfigDefaults(t *testing.T) {
	d := domain.DefaultWorkboardConfig()
	if d.WIPLimit != 3 || d.AnswerTimeoutMinutes != 10 || d.LimitCooldownMinutes != 60 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if d.Autonomous.Enabled {
		t.Fatal("autonomous should default off")
	}
}

func TestWorkboardAutonomousConfigJSONRoundTripsFalseBools(t *testing.T) {
	in := domain.WorkboardAutonomousConfig{
		Enabled: false,
		Mode:    domain.WorkboardAutonomousModeSkipTimeout,
		Sticky:  false,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"enabled":false`) || !strings.Contains(string(raw), `"sticky":false`) {
		t.Fatalf("false bools omitted: %s", raw)
	}
	var out domain.WorkboardAutonomousConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Enabled || out.Sticky {
		t.Fatalf("round-trip lost false: %+v", out)
	}
}
