package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// GenerateKey creates a fresh signing pair in the encodings the rest of this
// package accepts: base64 for both halves, so a GitHub Secret can hold either
// verbatim.
func GenerateKey() (publicB64, privateB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub),
		base64.StdEncoding.EncodeToString(priv), nil
}

// DefaultKeyPath is where `release-keygen` writes the private key when the
// caller does not name a file: ~/.config/today/release.key. It is deliberately
// outside the repository, so a careless `git add` cannot pick it up.
func DefaultKeyPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "today", "release.key")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "today", "release.key")
}

// WriteKeyFile stores the private key in a 0600 file and prints nothing. The
// caller decides what to tell the user.
func WriteKeyFile(path, privateB64 string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(privateB64+"\n"), 0o600)
}

// SignDir writes SHA256SUMS for every artifact in dir and SHA256SUMS.sig next
// to it. `keyText` is the private key in any accepted encoding.
func SignDir(dir, keyText string) (sums []byte, sig string, err error) {
	key, err := DecodePrivateKey(keyText)
	if err != nil {
		return nil, "", err
	}
	sums, err = WriteSums(dir)
	if err != nil {
		return nil, "", err
	}
	sig = SignChecksums(key, sums)
	if err := os.WriteFile(filepath.Join(dir, ChecksumsName), sums, 0o644); err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(dir, ChecksumsName+".sig"), []byte(sig+"\n"), 0o644); err != nil {
		return nil, "", err
	}
	return sums, sig, nil
}

// CheckDir verifies an existing release directory against `publicB64`.
func CheckDir(dir, publicB64 string) error {
	pub, err := LoadPublicKey(publicB64)
	if err != nil {
		return err
	}
	sums, err := os.ReadFile(filepath.Join(dir, ChecksumsName))
	if err != nil {
		return fmt.Errorf("release: %w", err)
	}
	sig, err := os.ReadFile(filepath.Join(dir, ChecksumsName+".sig"))
	if err != nil {
		return fmt.Errorf("release: %w", err)
	}
	if err := VerifyChecksums(pub, sums, sig); err != nil {
		return err
	}
	entries, err := ParseSums(sums)
	if err != nil {
		return err
	}
	for name, want := range entries {
		got, err := FileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("release: %s 的校验和不符", name)
		}
	}
	return nil
}
