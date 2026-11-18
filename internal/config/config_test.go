package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	f, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.ListenAddr != "" {
		t.Fatalf("ListenAddr = %q, want empty", f.ListenAddr)
	}
}

func TestSaveThenLoad(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, File{ListenAddr: ":8788"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	f, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.ListenAddr != ":8788" {
		t.Fatalf("ListenAddr = %q, want :8788", f.ListenAddr)
	}
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatalf("stat %s: %v", Path(dir), err)
	}
	// No temp files survive a successful save.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("leftover files: %v", names)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Today")
	if err := Save(dir, File{ListenAddr: "8787"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

func TestLoadRejectsBrokenJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("want error for broken json")
	}
}
