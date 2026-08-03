// Package directorassets embeds the built Director agent bundle and installs it
// into the AO data dir at daemon boot. It mirrors internal/skillassets: sessions
// run in a worktree of whatever project spawned them, so only an absolute path
// under the data dir resolves reliably. The embedded copy is the single source
// of truth and Install clobbers the on-disk copy every boot, so a new daemon
// build always refreshes it and the two cannot drift.
//
// The bundle is a self-contained esm file. The Director's @langchain/*
// dependencies are installed as a production node_modules via `npm install --omit=dev`
// in the director package directory. The Go adapter runs
// `node <dataDir>/director/dist/index.js` with NODE_PATH pointing to
// <dataDir>/director/node_modules.
package directorassets

import (
	"archive/tar"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed bundle/index.js bundle/nodemodules.tar bundle/skills/adhd/SKILL.md bundle/skills/git-workflow-and-versioning/SKILL.md
var bundle embed.FS

// DirName is the installed bundle's directory name under <dataDir>.
const DirName = "director"

// Dir returns the absolute directory the Director package is installed into.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, DirName)
}

// EntrypointPath returns the absolute path to the Director entrypoint the
// adapter launches with node.
func EntrypointPath(dataDir string) string {
	return filepath.Join(Dir(dataDir), "dist", "index.js")
}

// Install writes the embedded bundle and production node_modules into
// <dataDir>/director, replacing any existing copy. It runs at daemon boot
// before any session spawns.
func Install(dataDir string) error {
	dest := Dir(dataDir)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("create director dir %q: %w", dest, err)
	}
	// Extract the bundle (dist/index.js).
	bundleFile, err := bundle.Open("bundle/index.js")
	if err != nil {
		return fmt.Errorf("open embedded bundle: %w", err)
	}
	distDir := filepath.Join(dest, "dist")
	if err := os.MkdirAll(distDir, 0o750); err != nil {
		bundleFile.Close()
		return fmt.Errorf("create director dist dir: %w", err)
	}
	destIndex := filepath.Join(distDir, "index.js")
	f, err := os.OpenFile(destIndex, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		bundleFile.Close()
		return fmt.Errorf("write %q: %w", destIndex, err)
	}
	_, err = io.Copy(f, bundleFile)
	cerr := f.Close()
	bundleFile.Close()
	if err != nil {
		return fmt.Errorf("copy bundle: %w", err)
	}
	if cerr != nil {
		return fmt.Errorf("close bundle: %w", cerr)
	}
	if err := installSkill("bundle/skills/adhd/SKILL.md", filepath.Join(dest, "skills", "adhd", "SKILL.md")); err != nil {
		return err
	}
	if err := installSkill("bundle/skills/git-workflow-and-versioning/SKILL.md", filepath.Join(dest, "skills", "git-workflow-and-versioning", "SKILL.md")); err != nil {
		return err
	}
	// Extract node_modules from the embedded tar archive.
	nodemodules, err := bundle.Open("bundle/nodemodules.tar")
	if err != nil {
		return fmt.Errorf("open embedded node_modules tar: %w", err)
	}
	defer nodemodules.Close()
	gr, err := gzip.NewReader(nodemodules)
	if err != nil {
		return fmt.Errorf("gzip reader for node_modules: %w", err)
	}
	defer gr.Close()
	if err := extractTar(gr, dest); err != nil {
		return fmt.Errorf("extract node_modules tar: %w", err)
	}
	return nil
}

func installSkill(source, destination string) error {
	file, err := bundle.Open(source)
	if err != nil {
		return fmt.Errorf("open Director skill %q: %w", source, err)
	}
	defer file.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("create Director skill directory: %w", err)
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write Director skill %q: %w", destination, err)
	}
	_, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy Director skill: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Director skill: %w", closeErr)
	}
	return nil
}

// extractTar untars a gzip-compressed tar archive from r into dest,
// preserving symlinks and special files. Missing parent directories are
// created automatically.
func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		target := filepath.Join(dest, hdr.Name)
		// Security: refuse absolute paths or traversal attempts. Clean the name first
		// to handle tar entries like "./" that filepath.Clean normalizes to ".".
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || strings.Contains(name, "..") {
			return fmt.Errorf("tar entry %q: invalid path", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&0o777); err != nil {
				return fmt.Errorf("tar mkdir %q: %w", hdr.Name, err)
			}
		case tar.TypeSymlink:
			// Remove existing symlink if present so we can replace it.
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("tar symlink %q -> %q: %w", hdr.Name, hdr.Linkname, err)
			}
		case tar.TypeLink:
			os.Remove(target)
			if err := os.Link(filepath.Join(dest, hdr.Linkname), target); err != nil {
				return fmt.Errorf("tar link %q -> %q: %w", hdr.Name, hdr.Linkname, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return fmt.Errorf("tar mkdir parent %q: %w", hdr.Name, err)
			}
			w, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o666)
			if err != nil {
				return fmt.Errorf("tar open %q: %w", hdr.Name, err)
			}
			_, err = io.Copy(w, tr)
			cerr := w.Close()
			if err != nil {
				return fmt.Errorf("tar write %q: %w", hdr.Name, err)
			}
			if cerr != nil {
				return fmt.Errorf("tar close %q: %w", hdr.Name, cerr)
			}
		default:
			// Skip unsupported entry types (e.g. char device, block device).
		}
	}
	return nil
}
