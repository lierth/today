// Package release implements the signed-release format: the ed25519 signature
// over SHA256SUMS, the checksum file itself, and the version comparison the
// updater uses. It is shared by `todayd release-sign`, the CI script and the
// app's updater, so the three cannot drift.
package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PublicKey is the build-in trust anchor. Release builds overwrite it with
// -ldflags -X; the value in the repository is the maintainer's key so a local
// `make release` round-trips.
var PublicKey = ""

// SigningKeyEnv is the environment variable a CI secret is read from. A local
// key lives in a file (release.DefaultKeyPath), never in the repository.
const SigningKeyEnv = "TODAY_RELEASE_KEY"

// ChecksumsName is the file the signature covers.
const ChecksumsName = "SHA256SUMS"

// LoadPublicKey decodes the base64 public key, either the built-in one or an
// explicit override (tests, forks).
func LoadPublicKey(raw string) (ed25519.PublicKey, error) {
	if raw == "" {
		raw = PublicKey
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("release: no public key built in")
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("release: public key is not 32 bytes of base64")
	}
	return ed25519.PublicKey(b), nil
}

// DecodePrivateKey accepts base64 (std or raw) or hex, so a CI secret can be
// pasted without the operator having to guess the encoding.
func DecodePrivateKey(raw string) (ed25519.PrivateKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("release: no signing key")
	}
	for _, dec := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		hex.DecodeString,
	} {
		if b, err := dec(raw); err == nil && len(b) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(b), nil
		}
	}
	return nil, errors.New("release: signing key must be 64 bytes (base64 or hex)")
}

// SignChecksums signs the bytes of SHA256SUMS and returns the base64
// signature, which the release publishes as SHA256SUMS.sig.
func SignChecksums(key ed25519.PrivateKey, sums []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, sums))
}

// VerifyChecksums checks SHA256SUMS against its signature file. It returns a
// plain error so the caller can show it verbatim.
func VerifyChecksums(pub ed25519.PublicKey, sums, sig []byte) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil {
		return errors.New("签名文件不是有效的 base64")
	}
	if len(raw) != ed25519.SignatureSize {
		return errors.New("签名长度不对")
	}
	if !ed25519.Verify(pub, sums, raw) {
		return errors.New("签名校验失败：发布清单被改动过，或不是用这把密钥签的")
	}
	return nil
}

// Sums is a parsed SHA256SUMS: file name → lowercase hex digest.
type Sums map[string]string

// ParseSums reads the `<hex>  <name>` format sha256sum writes (two spaces, or
// one with a leading `*` for binary mode).
func ParseSums(data []byte) (Sums, error) {
	out := Sums{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		digest, name, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("release: bad checksum line %q", line)
		}
		name = strings.TrimPrefix(strings.TrimSpace(name), "*")
		digest = strings.ToLower(digest)
		if len(digest) != 64 {
			return nil, fmt.Errorf("release: bad digest for %q", name)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return nil, fmt.Errorf("release: bad digest for %q", name)
		}
		out[name] = digest
	}
	if len(out) == 0 {
		return nil, errors.New("release: empty checksum file")
	}
	return out, nil
}

// WriteSums lists every regular file in dir (not recursive), sorted by name.
func WriteSums(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == ChecksumsName || name == ChecksumsName+".sig" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		digest, err := FileSHA256(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		b.WriteString(digest)
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

// FileSHA256 hashes one file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Version is a parsed dotted version. Pre-release labels sort before the
// release itself ("0.4.0-rc1" < "0.4.0"), like semver.
type Version struct {
	Numbers [3]int
	Pre     string
	OK      bool
}

// ParseVersion accepts v1.2.3, 1.2.3, 1.2, 1 and a -pre / +build suffix.
func ParseVersion(raw string) Version {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	out := Version{}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		out.Pre = v[i+1:]
		v = v[:i]
	}
	if v == "" {
		return out
	}
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return out
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out
		}
		out.Numbers[i] = n
	}
	out.OK = true
	return out
}

// CompareVersions returns -1, 0 or 1. An unparsable side compares as smaller so
// a broken tag never wins.
func CompareVersions(a, b string) int {
	av, bv := ParseVersion(a), ParseVersion(b)
	switch {
	case !av.OK && !bv.OK:
		return 0
	case !av.OK:
		return -1
	case !bv.OK:
		return 1
	}
	for i := range 3 {
		if av.Numbers[i] != bv.Numbers[i] {
			if av.Numbers[i] < bv.Numbers[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case av.Pre == bv.Pre:
		return 0
	case av.Pre == "":
		return 1
	case bv.Pre == "":
		return -1
	case av.Pre < bv.Pre:
		return -1
	}
	return 1
}

// IsNewer reports whether latest is strictly newer than current. A dev build
// never updates itself, and a pre-release is ignored unless allowPre.
func IsNewer(latest, current string, allowPre bool) bool {
	if current == "" || current == "dev" {
		return false
	}
	lv := ParseVersion(latest)
	if !lv.OK {
		return false
	}
	if lv.Pre != "" && !allowPre {
		return false
	}
	return CompareVersions(latest, current) > 0
}

// StrictTag reports whether raw is exactly the shape a release tag may have:
// an optional "v", then three dot-separated numbers. Anything else (a build
// suffix, whitespace, a path separator, "..") is refused before it can reach a
// file name.
func StrictTag(raw string) bool {
	return strictTagRe.MatchString(raw)
}

var strictTagRe = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)

// UpdateRoot is where update downloads are staged.
func UpdateRoot(cacheRoot string) string {
	return filepath.Join(cacheRoot, "Today", "updates")
}

// UpdateDirFor returns <cacheRoot>/Today/updates/<version> after proving the
// result is inside the updates directory. A version that would escape it (via
// ".." or an absolute path) is an error, not a sanitised name: silently
// rewriting it would hide a malicious feed.
func UpdateDirFor(cacheRoot, version string) (string, error) {
	if !StrictTag(version) {
		return "", fmt.Errorf("release: 版本号格式不合法：%q", version)
	}
	root := UpdateRoot(cacheRoot)
	dir := filepath.Join(root, strings.TrimPrefix(version, "v"))
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("release: 下载目录越界：%q", version)
	}
	return dir, nil
}

// RemoveUpdateDir deletes one staged version after re-checking that it really
// is inside the updates root. This is the last line of defence before an
// os.RemoveAll: a mistake here would delete the person's files.
func RemoveUpdateDir(cacheRoot, version string) error {
	dir, err := UpdateDirFor(cacheRoot, version)
	if err != nil {
		return err
	}
	root := UpdateRoot(cacheRoot)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		rel == "." {
		return fmt.Errorf("release: 拒绝删除 %q", dir)
	}
	return os.RemoveAll(dir)
}

// AssetFor picks the archive for this platform: Today-<ver>-arm64.zip. A
// universal name (Today-<ver>.zip) is accepted as a fallback.
func AssetFor(names []string, version string) string {
	want := []string{
		"Today-" + version + "-arm64.zip",
		"Today-" + strings.TrimPrefix(version, "v") + "-arm64.zip",
	}
	for _, w := range want {
		for _, n := range names {
			if n == w {
				return n
			}
		}
	}
	return ""
}

// FirmwareName is the OTA image the app downloads and hands to a board: the
// application only, at the app partition offset.
func FirmwareName(version string) string {
	return "today-rlcd-" + strings.TrimPrefix(version, "v") + ".bin"
}

// FullFirmwareName is the merged image (bootloader, partitions, boot_app0,
// application) for flashing over USB from offset 0. It must never be written
// to the OTA slot.
func FullFirmwareName(version string) string {
	return "today-rlcd-" + strings.TrimPrefix(version, "v") + "-full.bin"
}
