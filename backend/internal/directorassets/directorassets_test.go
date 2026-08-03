package directorassets_test

import (
	"os"
	"path/filepath"
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
	if want := filepath.Join(dir, "director", "index.js"); path != want {
		t.Fatalf("EntrypointPath = %q, want %q", path, want)
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
