package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// The keygen → sign → verify round trip, using only the files the tool writes.
func TestReleaseSignRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := runReleaseKeygen([]string{"--out", keyFile}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v, want 0600", st.Mode().Perm())
	}
	priv, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(string(priv[:len(priv)-1]))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		t.Fatalf("key file is not base64 ed25519: %v", err)
	}
	pub := base64.StdEncoding.EncodeToString(ed25519.PrivateKey(raw).Public().(ed25519.PublicKey))

	art := filepath.Join(dir, "Today-0.4.0-arm64.zip")
	if err := os.WriteFile(art, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TODAY_RELEASE_KEY", string(priv))
	if err := runReleaseSign([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if err := runReleaseVerify([]string{dir, "--public-key", pub}); err != nil {
		t.Fatalf("verify with the matching key: %v", err)
	}

	// A changed artifact fails verification.
	if err := os.WriteFile(art, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runReleaseVerify([]string{dir, "--public-key", pub}); err == nil {
		t.Fatal("tampered artifact verified")
	}
}

// Signing without a key explains what to set rather than failing obscurely.
func TestReleaseSignNeedsKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TODAY_RELEASE_KEY", "")
	if err := runReleaseSign([]string{dir}); err == nil {
		t.Fatal("signing without a key succeeded")
	}
}

// `--key-file` works, and so does the older "directory first" order: Go's flag
// package leaves both in different places.
func TestReleaseSignKeyFileAndOrder(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := runReleaseKeygen([]string{"--out", keyFile}); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(dir, "today-rlcd-0.4.0.bin")
	if err := os.WriteFile(art, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runReleaseSign([]string{"--key-file", keyFile, dir}); err != nil {
		t.Fatalf("flags before dir: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "SHA256SUMS")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "SHA256SUMS.sig")); err != nil {
		t.Fatal(err)
	}
	if err := runReleaseSign([]string{dir, "--key-file", keyFile}); err != nil {
		t.Fatalf("dir before flags: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SHA256SUMS.sig")); err != nil {
		t.Fatal("no signature after the second order")
	}
}
