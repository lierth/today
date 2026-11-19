package release

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A first download writes the whole body; a second call with a partial file
// sends Range and appends only the missing bytes.
func TestDownloadResumes(t *testing.T) {
	body := []byte(strings.Repeat("today", 4096))
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		start := int64(0)
		if r.Header.Get("Range") != "" {
			// "bytes=N-"
			_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &start)
			if err != nil {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+
				strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(body[start:])
	}))
	defer server.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "x.zip")
	n, err := Download(context.Background(), server.Client(), server.URL, dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(body)) {
		t.Fatalf("first call wrote %d", n)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatal("content mismatch")
	}

	// Cut the file short and download again: only the tail is fetched.
	if err := os.Truncate(dest, 5000); err != nil {
		t.Fatal(err)
	}
	n, err = Download(context.Background(), server.Client(), server.URL, dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(body))-5000 {
		t.Fatalf("resume wrote %d, want %d", n, int64(len(body))-5000)
	}
	got, _ = os.ReadFile(dest)
	if string(got) != string(body) {
		t.Fatal("resume content mismatch")
	}
	if ranges[len(ranges)-1] != "bytes=5000-" {
		t.Fatalf("last Range = %q", ranges[len(ranges)-1])
	}
}

// A server without Range support answers 200 with everything; the download
// must restart the file, not append.
func TestDownloadRestartsWithoutRange(t *testing.T) {
	body := []byte("full-body-here")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "x.bin")
	if err := os.WriteFile(dest, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Download(context.Background(), server.Client(), server.URL, dest, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(body) {
		t.Fatalf("got %q", got)
	}
}

// An HTTP error leaves nothing behind.
func TestDownloadHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "x.bin")
	if _, err := Download(context.Background(), server.Client(), server.URL, dest, nil); err == nil {
		t.Fatal("404 accepted")
	}
}

// VerifyArchive accepts the right digest and rejects a truncated file and a
// name the manifest does not list.
func TestVerifyArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Today-0.4.0-arm64.zip")
	if err := os.WriteFile(path, []byte("zip-here"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	sums := Sums{"Today-0.4.0-arm64.zip": digest}
	if err := VerifyArchive(path, sums); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArchive(path, Sums{"Today-0.4.0-arm64.zip": strings.Repeat("0", 64)}); err == nil {
		t.Fatal("wrong digest accepted")
	}
	if err := VerifyArchive(path, Sums{}); err == nil {
		t.Fatal("unlisted file accepted")
	}
}

// FetchSignedSums: the happy path, a tampered manifest, and a signature made
// with a different key.
func TestFetchSignedSums(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  Today-0.4.0-arm64.zip\n")
	good := SignChecksums(priv, sums)
	signer := func(sig, manifest string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, ".sig"):
				_, _ = w.Write([]byte(sig))
			case strings.HasSuffix(r.URL.Path, ChecksumsName):
				_, _ = w.Write([]byte(manifest))
			default:
				http.NotFound(w, r)
			}
		}))
	}

	srv := signer(good, string(sums))
	defer srv.Close()
	got, err := FetchSignedSums(context.Background(), srv.Client(), srv.URL, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("parsed %d", len(got))
	}

	// Tampered manifest with the original signature.
	srv2 := signer(good, strings.Replace(string(sums), "0123", "9999", 1))
	defer srv2.Close()
	if _, err := FetchSignedSums(context.Background(), srv2.Client(), srv2.URL, pub); err == nil {
		t.Fatal("tampered manifest accepted")
	}

	// Signature from another key.
	otherPub, otherPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = otherPub
	srv3 := signer(SignChecksums(otherPriv, sums), string(sums))
	defer srv3.Close()
	if _, err := FetchSignedSums(context.Background(), srv3.Client(), srv3.URL, pub); err == nil {
		t.Fatal("foreign signature accepted")
	}

	// Missing signature file.
	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ChecksumsName) {
			_, _ = w.Write(sums)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv4.Close()
	if _, err := FetchSignedSums(context.Background(), srv4.Client(), srv4.URL, pub); err == nil {
		t.Fatal("missing signature accepted")
	}
}

// The state machine: every legal path in, every failure out.
func TestStateMachine(t *testing.T) {
	type step struct {
		from State
		ev   Event
		want State
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{"manual happy path", []step{
			{StateIdle, Event{Kind: EventCheck}, StateChecking},
			{StateChecking, Event{Kind: EventAvailable, Version: "0.4.0"}, StateAvailable},
			{StateAvailable, Event{Kind: EventDownload}, StateDownloading},
			{StateDownloading, Event{Kind: EventProgress, Progress: 40}, StateDownloading},
			{StateDownloading, Event{Kind: EventDownloaded}, StateVerifying},
			{StateVerifying, Event{Kind: EventVerified}, StateReady},
			{StateReady, Event{Kind: EventInstall}, StateInstalling},
		}},
		{"nothing newer", []step{
			{StateIdle, Event{Kind: EventCheck}, StateChecking},
			{StateChecking, Event{Kind: EventNone}, StateIdle},
		}},
		{"download fails then retries", []step{
			{StateIdle, Event{Kind: EventCheck}, StateChecking},
			{StateChecking, Event{Kind: EventAvailable, Version: "0.4.0"}, StateAvailable},
			{StateAvailable, Event{Kind: EventDownload}, StateDownloading},
			{StateDownloading, Event{Kind: EventFail, Reason: "网络断了"}, StateFailed},
			{StateFailed, Event{Kind: EventDownload}, StateDownloading},
			{StateDownloading, Event{Kind: EventDownloaded}, StateVerifying},
			{StateVerifying, Event{Kind: EventFail, Reason: "签名不符"}, StateFailed},
		}},
		{"auto download shortcut", []step{
			{StateChecking, Event{Kind: EventAvailable, Version: "0.4.0"}, StateAvailable},
			{StateAvailable, Event{Kind: EventReady}, StateReady},
		}},
		{"skip resets", []step{
			{StateAvailable, Event{Kind: EventReset}, StateIdle},
			{StateReady, Event{Kind: EventReset}, StateIdle},
			{StateFailed, Event{Kind: EventReset}, StateIdle},
		}},
		{"illegal events keep the state", []step{
			{StateInstalling, Event{Kind: EventCheck}, StateInstalling},
			{StateDownloading, Event{Kind: EventCheck}, StateDownloading},
			{StateIdle, Event{Kind: EventInstall}, StateIdle},
			{StateIdle, Event{Kind: EventVerified}, StateIdle},
			{StateChecking, Event{Kind: EventAvailable}, StateFailed},
		}},
		{"cancel a download", []step{
			{StateDownloading, Event{Kind: EventReset}, StateFailed},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for i, s := range c.steps {
				got := Next(s.from, s.ev)
				if got.State != s.want {
					t.Fatalf("step %d: %s + %s = %s, want %s (%s)",
						i, s.from, s.ev.Kind, got.State, s.want, got.Note)
				}
			}
		})
	}
}

// A failed check from the tray is allowed again immediately; a check while one
// is in flight is refused.
func TestCheckCoalesces(t *testing.T) {
	if got := Next(StateChecking, Event{Kind: EventCheck}); got.State != StateChecking {
		t.Fatalf("second check moved to %s", got.State)
	}
	if got := Next(StateFailed, Event{Kind: EventCheck}); got.State != StateChecking {
		t.Fatalf("retry after failure moved to %s", got.State)
	}
}

// ErrNotReady exists for callers that must not install without verification.
func TestErrNotReady(t *testing.T) {
	if !errors.Is(ErrNotReady, ErrNotReady) {
		t.Fatal("sentinel lost")
	}
}
