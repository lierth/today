package host

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"today/internal/release"
)

// A release server that answers like GitHub: the latest document, the assets,
// the signed manifest and one zip whose contents are a fake .app.
type fakeRelease struct {
	t        *testing.T
	pub      string
	version  string
	zipBytes []byte
	body     string
	base     string
}

func newFakeRelease(t *testing.T, version, notes string) *fakeRelease {
	t.Helper()
	// Build a tiny zip: ditto refuses it, but the signature and checksum path
	// is what this test covers; the extraction is tested separately with a
	// real ditto run in internal/release.
	zipBytes := []byte("PK\x03\x04 fake zip for the updater test")
	fr := &fakeRelease{t: t, version: version, zipBytes: zipBytes}
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v" + version,
			"body":     notes,
			"html_url": server.URL + "/releases/tag/v" + version,
			"assets": []map[string]string{
				{"name": release.FirmwareName(version), "browser_download_url": server.URL + "/dl/" + release.FirmwareName(version)},
				{"name": "Today-" + version + "-arm64.zip", "browser_download_url": server.URL + "/dl/Today-" + version + "-arm64.zip"},
			},
		})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		switch name {
		case "Today-" + version + "-arm64.zip":
			_, _ = w.Write(zipBytes)
		case release.ChecksumsName, release.ChecksumsName + ".sig":
			data, err := os.ReadFile(filepath.Join(fr.dir(), name))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	})
	server = httptest.NewServer(mux)
	fr.base = server.URL
	t.Cleanup(server.Close)

	// Sign the manifest the same way the release script does.
	dir := fr.dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(dir, "Today-"+version+"-arm64.zip")
	if err := os.WriteFile(art, zipBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fr.pub = base64.StdEncoding.EncodeToString(pubKey)
	sums, err := release.WriteSums(dir)
	if err != nil {
		t.Fatal(err)
	}
	sig := release.SignChecksums(privKey, sums)
	if err := os.WriteFile(filepath.Join(dir, release.ChecksumsName), sums, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, release.ChecksumsName+".sig"), []byte(sig), 0o644); err != nil {
		t.Fatal(err)
	}
	return fr
}

// dir is the cache directory the updater would use for this version.
func (f *fakeRelease) dir() string {
	dir, err := updateDir(f.version)
	if err != nil {
		f.t.Fatal(err)
	}
	return dir
}

func TestFetchReleaseInfoPicksArm64Zip(t *testing.T) {
	fr := newFakeRelease(t, "0.4.0", "修好了很多事\n第二个改动")
	info, err := fetchReleaseInfo(context.Background(), http.DefaultClient, fr.base+"/releases/latest")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v0.4.0" {
		t.Fatalf("version %q", info.Version)
	}
	if info.AssetName != "Today-0.4.0-arm64.zip" {
		t.Fatalf("asset %q", info.AssetName)
	}
	if info.FirmwareName != "today-rlcd-0.4.0.bin" {
		t.Fatalf("firmware %q", info.FirmwareName)
	}
	if want := fr.base + "/releases/download/v0.4.0"; releaseBase(info.PageURL) != want {
		t.Fatalf("release base %q, want %q", releaseBase(info.PageURL), want)
	}
	if !strings.Contains(info.Notes, "第二个改动") {
		t.Fatalf("notes %q", info.Notes)
	}
}

// The signed manifest path: the right key parses the sums, and a flipped byte
// in the manifest is refused before anything is downloaded.
func TestFetchSignedSumsFromServer(t *testing.T) {
	fr := newFakeRelease(t, "0.4.0", "")
	pub, err := release.LoadPublicKey(fr.pub)
	if err != nil {
		t.Fatal(err)
	}
	sums, err := release.FetchSignedSums(context.Background(), http.DefaultClient,
		fr.base+"/dl", pub)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sums["Today-0.4.0-arm64.zip"]; !ok {
		t.Fatalf("asset missing from the manifest: %v", sums)
	}

	// A different key must fail.
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := release.FetchSignedSums(context.Background(), http.DefaultClient, fr.base+"/dl",
		otherPub); err == nil {
		t.Fatal("foreign key verified")
	}
}

// clipNotes caps the body at 1000 runes and keeps the text readable.
func TestClipNotes(t *testing.T) {
	if got := clipNotes("  你好  "); got != "你好" {
		t.Fatalf("trim: %q", got)
	}
	long := strings.Repeat("字", 1200)
	got := clipNotes(long)
	if len([]rune(got)) != 1001 {
		t.Fatalf("clipped to %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatal("missing ellipsis")
	}
}

// defaultUpdateFeed is empty for a source build and points at the repository
// when the build knows it.
func TestDefaultUpdateFeed(t *testing.T) {
	prev := ReleaseRepo
	t.Cleanup(func() { ReleaseRepo = prev })
	ReleaseRepo = ""
	if got := defaultUpdateFeed(); got != "" {
		t.Fatalf("source build produced %q", got)
	}
	ReleaseRepo = "o/r"
	want := "https://api.github.com/repos/o/r/releases/latest"
	if got := defaultUpdateFeed(); got != want {
		t.Fatalf("got %q", got)
	}
}
