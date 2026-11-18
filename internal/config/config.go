// Package config holds the few values that must be readable before the
// database is open. The listen address is here on purpose: a port occupied by
// another program would otherwise leave the app with no way in, so it can be
// corrected in a plain file instead of a web page that cannot be reached.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName is the file consulted at boot, next to the database.
const FileName = "config.json"

// File mirrors the pre-boot settings.
type File struct {
	ListenAddr string `json:"listen_addr,omitempty"`
}

// Path is the config file location for a data directory.
func Path(dataDir string) string { return filepath.Join(dataDir, FileName) }

// Load reads the file. A missing file is not an error: it returns an empty
// File so the caller falls through to its default.
func Load(dataDir string) (File, error) {
	var f File
	b, err := os.ReadFile(Path(dataDir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return f, nil
		}
		return f, fmt.Errorf("config: read: %w", err)
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("config: %s: %w", Path(dataDir), err)
	}
	return f, nil
}

// Save writes the file atomically; a half-written file would break the next
// boot, which is exactly when nobody can fix it from the UI.
func Save(dataDir string, f File) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("config: dir: %w", err)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dataDir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("config: temp: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close: %w", err)
	}
	if err := os.Rename(name, Path(dataDir)); err != nil {
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}
