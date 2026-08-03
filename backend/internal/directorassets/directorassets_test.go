package directorassets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/directorassets"
)

func TestInstallWritesEntrypoint(t *testing.T) {
	dir := t.TempDir()
	if err := directorassets.Install(dir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	path := directorassets.EntrypointPath(dir)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat entrypoint: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("installed entrypoint is empty")
	}
	if want := filepath.Join(dir, "director", "dist", "index.js"); path != want {
		t.Fatalf("EntrypointPath = %q, want %q", path, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "director", "skills", "adhd", "SKILL.md")); err != nil {
		t.Fatalf("stat installed ADHD skill: %v", err)
	}
	gitSkillPath := filepath.Join(dir, "director", "skills", "git-workflow-and-versioning", "SKILL.md")
	gitSkill, err := os.ReadFile(gitSkillPath)
	if err != nil {
		t.Fatalf("stat installed Git workflow skill: %v", err)
	}
	if !strings.Contains(string(gitSkill), "# Git Workflow and Versioning") {
		t.Fatal("installed Git workflow skill is incomplete")
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := directorassets.Install(dir); err != nil {
			t.Fatalf("Install (run %d): %v", i, err)
		}
	}
	if _, err := os.Stat(directorassets.EntrypointPath(dir)); err != nil {
		t.Fatalf("stat after reinstall: %v", err)
	}
}
