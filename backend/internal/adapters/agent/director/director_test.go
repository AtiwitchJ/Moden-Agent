package director_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/adapters/agent/director"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

func TestManifestID(t *testing.T) {
	if got := director.New().Manifest().ID; got != string(domain.HarnessDirector) {
		t.Fatalf("manifest id = %q, want %q", got, domain.HarnessDirector)
	}
}

func TestGetLaunchCommandRunsTheInstalledBundle(t *testing.T) {
	p := director.New(director.WithDataDir("/data"))
	argv, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("GetLaunchCommand: %v", err)
	}
	if len(argv) != 2 || argv[0] != "node" {
		t.Fatalf("argv = %v, want [node <path>]", argv)
	}
	if want := filepath.Join("/data", "director", "index.js"); argv[1] != want {
		t.Fatalf("entrypoint = %q, want %q", argv[1], want)
	}
}

func TestGetLaunchCommandRequiresDataDir(t *testing.T) {
	_, err := director.New().GetLaunchCommand(context.Background(), ports.LaunchConfig{SessionID: "sess-1"})
	if err == nil {
		t.Fatal("GetLaunchCommand: want error when the data dir is unset, got nil")
	}
	if !strings.Contains(err.Error(), "data dir") {
		t.Fatalf("error = %v, want it to name the missing data dir", err)
	}
}

func TestPromptDeliveryIsEnvBased(t *testing.T) {
	got, err := director.New().GetPromptDeliveryStrategy(context.Background(), ports.LaunchConfig{})
	if err != nil {
		t.Fatalf("GetPromptDeliveryStrategy: %v", err)
	}
	if got != ports.PromptDeliveryAfterStart {
		t.Fatalf("strategy = %v, want %v", got, ports.PromptDeliveryAfterStart)
	}
}
