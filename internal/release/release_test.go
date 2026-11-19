package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func b64raw(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }

func hexLower(b []byte) string { return hex.EncodeToString(b) }

// A fresh key signs a checksum file and the matching public key verifies it; a
// flipped byte, a different key and a truncated signature all fail.
func TestSignAndVerifyChecksums(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("abc  Today-0.4.0-arm64.zip\n")
	sig := SignChecksums(priv, sums)
	if err := VerifyChecksums(pub, sums, []byte(sig)); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	// A tampered manifest.
	if err := VerifyChecksums(pub, []byte("def  Today-0.4.0-arm64.zip\n"), []byte(sig)); err == nil {
		t.Fatal("tampered manifest accepted")
	}
	// A different key.
	otherPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyChecksums(otherPub, sums, []byte(sig)); err == nil {
		t.Fatal("signature from another key accepted")
	}
	// Garbage signature text.
	for _, bad := range []string{"", "not base64!", "AAAA"} {
		if err := VerifyChecksums(pub, sums, []byte(bad)); err == nil {
			t.Fatalf("bad signature %q accepted", bad)
		}
	}
}

// The key file formats an operator is likely to paste (base64 std, base64 raw,
// hex) are all accepted; anything else is refused.
func TestDecodePrivateKeyFormats(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{
		b64(priv),
		b64raw(priv),
		hexLower(priv),
	}
	for i, text := range texts {
		got, err := DecodePrivateKey(text)
		if err != nil {
			t.Fatalf("format %d: %v", i, err)
		}
		if !got.Equal(priv) {
			t.Fatalf("format %d decoded a different key", i)
		}
	}
	if _, err := DecodePrivateKey("short"); err == nil {
		t.Fatal("short key accepted")
	}
	if _, err := DecodePrivateKey(""); err == nil {
		t.Fatal("empty key accepted")
	}
}

func TestParseSums(t *testing.T) {
	data := []byte("" +
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  Today-0.4.0-arm64.zip\n" +
		"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210 *today-rlcd-0.4.0.bin\n" +
		"# a comment\n" +
		"\n")
	sums, err := ParseSums(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 {
		t.Fatalf("parsed %d entries", len(sums))
	}
	if sums["Today-0.4.0-arm64.zip"] != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("zip digest: %q", sums["Today-0.4.0-arm64.zip"])
	}
	if _, ok := sums["today-rlcd-0.4.0.bin"]; !ok {
		t.Fatal("binary-mode line dropped")
	}
	for _, bad := range []string{
		"",
		"onlyonefield\n",
		"zz  file\n",
		"ABC  file\n",
	} {
		if _, err := ParseSums([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// WriteSums hashes every regular file, sorted, and leaves itself out.
func TestWriteSums(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"b.txt": "bravo",
		"a.txt": "alpha",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := WriteSums(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Index(got, "a.txt") > strings.Index(got, "b.txt") {
		t.Fatalf("sums not sorted:\n%s", got)
	}
	if _, err := ParseSums(data); err != nil {
		t.Fatalf("round trip: %v", err)
	}
}

// Version ordering: numeric parts first, then a pre-release sorts before the
// release, and junk is never newer.
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.4.0", "0.3.9", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0", 1},
		{"0.4.0-rc1", "0.4.0", -1},
		{"0.4.0", "0.4.0-rc1", 1},
		{"0.4.0-rc1", "0.4.0-rc2", -1},
		{"junk", "1.0.0", -1},
		{"1.0.0", "junk", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		allowPre        bool
		want            bool
	}{
		{"0.4.0", "0.3.0", false, true},
		{"0.3.0", "0.3.0", false, false},
		{"0.2.0", "0.3.0", false, false},
		{"dev", "0.3.0", false, false},
		{"0.4.0", "dev", false, false},
		{"0.4.0-rc1", "0.3.0", false, false},
		{"0.4.0-rc1", "0.3.0", true, true},
		{"garbage", "0.3.0", false, false},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current, c.allowPre); got != c.want {
			t.Errorf("IsNewer(%q,%q,%v) = %v", c.latest, c.current, c.allowPre, got)
		}
	}
}

// Only the arm64 zip is installed; a source tarball or a linux build is not an
// asset this updater understands.
func TestAssetFor(t *testing.T) {
	names := []string{
		"Today-0.4.0.zip", "Today-0.4.0-arm64.zip", "Today-0.4.0.dmg",
		"today-rlcd-0.4.0.bin", "SHA256SUMS", "SHA256SUMS.sig",
	}
	if got := AssetFor(names, "0.4.0"); got != "Today-0.4.0-arm64.zip" {
		t.Fatalf("AssetFor = %q", got)
	}
	if got := AssetFor(names, "v0.4.0"); got != "Today-0.4.0-arm64.zip" {
		t.Fatalf("v-prefixed AssetFor = %q", got)
	}
	if got := AssetFor(names, "0.5.0"); got != "" {
		t.Fatalf("missing version picked %q", got)
	}
	if got := FirmwareName("v0.4.0"); got != "today-rlcd-0.4.0.bin" {
		t.Fatalf("FirmwareName = %q", got)
	}
}

// LoadPublicKey refuses empty and wrong-length values.
func TestLoadPublicKey(t *testing.T) {
	if _, err := LoadPublicKey(""); err == nil {
		t.Fatal("empty key accepted")
	}
	if _, err := LoadPublicKey("AAAA"); err == nil {
		t.Fatal("short key accepted")
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadPublicKey(b64(pub))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(pub) {
		t.Fatal("key round trip changed the bytes")
	}
}

// A version is used as a directory name and inside a URL: only three numbers
// with an optional v are allowed, so "v9.9.9+/../../../.." cannot escape.
func TestStrictTag(t *testing.T) {
	ok := []string{"1.2.3", "v0.4.0", "v10.20.30"}
	bad := []string{
		"", "v", "1.2", "1.2.3.4", "v1.2.3-rc1", "v1.2.3+build",
		"v9.9.9+/../../../..", "v1.2.3/../../x", "../1.2.3", "v1.2.3 ",
		"V1.2.3", "1.2.-3", "v1.2.03x",
	}
	for _, v := range ok {
		if !StrictTag(v) {
			t.Errorf("StrictTag(%q) = false", v)
		}
	}
	for _, v := range bad {
		if StrictTag(v) {
			t.Errorf("StrictTag(%q) = true", v)
		}
	}
}

// UpdateDirFor stays inside <cache>/Today/updates and rejects paths that would
// escape it. RemoveUpdateDir re-checks before deleting anything.
func TestUpdateDirContainment(t *testing.T) {
	root := t.TempDir()
	dir, err := UpdateDirFor(root, "v0.4.0")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "Today", "updates", "0.4.0")
	if dir != want {
		t.Fatalf("UpdateDirFor = %q, want %q", dir, want)
	}
	for _, bad := range []string{"../x", "v1.2.3/../../..", "/etc/passwd", "v1.2.3+evil"} {
		if _, err := UpdateDirFor(root, bad); err == nil {
			t.Errorf("UpdateDirFor(%q) accepted", bad)
		}
	}
	// RemoveUpdateDir deletes only the staged version.
	if err := os.MkdirAll(filepath.Join(dir, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(root, "Today", "keep-me")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveUpdateDir(root, "0.4.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("staged dir survived")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatal("RemoveUpdateDir deleted outside the version directory")
	}
	if err := RemoveUpdateDir(root, "../.."); err == nil {
		t.Fatal("RemoveUpdateDir accepted an escaping version")
	}
}
