package headless

import (
	"context"
	"reflect"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

func TestFromLaunchForcesBypassPermissions(t *testing.T) {
	var got ports.LaunchConfig
	launch := func(_ context.Context, cfg ports.LaunchConfig) ([]string, error) {
		got = cfg
		return []string{"agent", "--print", cfg.Prompt}, nil
	}

	argv, err := FromLaunch(context.Background(), launch, ports.LaunchConfig{
		Prompt:      "do it",
		Permissions: ports.PermissionModeAcceptEdits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Permissions != ports.PermissionModeBypassPermissions {
		t.Fatalf("permissions = %q, want bypassPermissions: a headless run has nobody to answer an approval", got.Permissions)
	}
	if !reflect.DeepEqual(argv, []string{"agent", "--print", "do it"}) {
		t.Fatalf("argv = %#v", argv)
	}
}

func TestFromLaunchRequiresAPrompt(t *testing.T) {
	launch := func(context.Context, ports.LaunchConfig) ([]string, error) {
		t.Fatal("launch must not be called without a prompt")
		return nil, nil
	}

	if _, err := FromLaunch(context.Background(), launch, ports.LaunchConfig{Prompt: "  "}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestFromLaunchPropagatesLaunchErrors(t *testing.T) {
	launch := func(context.Context, ports.LaunchConfig) ([]string, error) {
		return nil, context.Canceled
	}

	if _, err := FromLaunch(context.Background(), launch, ports.LaunchConfig{Prompt: "do it"}); err == nil {
		t.Fatal("expected the launch error to propagate")
	}
}
