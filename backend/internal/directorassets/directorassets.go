// Package directorassets embeds the built Director agent bundle and installs it
// into the AO data dir at daemon boot. It mirrors internal/skillassets: sessions
// run in a worktree of whatever project spawned them, so only an absolute path
// under the data dir resolves reliably. The embedded copy is the single source
// of truth and Install clobbers the on-disk copy every boot, so a new daemon
// build always refreshes it and the two cannot drift.
package directorassets

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed bundle/index.js
var files embed.FS

// DirName is the installed bundle's directory name under <dataDir>.
const DirName = "director"

// Dir returns the absolute directory the bundle installs into.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, DirName)
}

// EntrypointPath returns the absolute path to the Director entrypoint the
// adapter launches with node.
func EntrypointPath(dataDir string) string {
	return filepath.Join(Dir(dataDir), "index.js")
}

// Install writes the embedded bundle into <dataDir>/director, replacing any
// existing copy. It runs at daemon boot before any session spawns.
func Install(dataDir string) error {
	dest := Dir(dataDir)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("create director dir %q: %w", dest, err)
	}
	b, err := files.ReadFile("bundle/index.js")
	if err != nil {
		return fmt.Errorf("read embedded director bundle: %w", err)
	}
	target := EntrypointPath(dataDir)
	if err := os.WriteFile(target, b, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", target, err)
	}
	return nil
}
