package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "0.1.9", true},
		{"1.0.0", "v1.0.0", false},
		{"v1.0.1", "1.0", true},
		{"dev", "dev", false},
		{"v2", "dev", false},
		{"nope", "1.0.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q,%q)=%v want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestFetchReleaseGitHubAndCustom(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "" {
			t.Error("missing accept")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.3.0","html_url":"https://example.com/release"}`))
	}))
	defer gh.Close()
	rel, err := fetchRelease(context.Background(), gh.URL, gh.Client())
	if err != nil || rel.Version != "v0.3.0" || rel.URL != "https://example.com/release" {
		t.Fatalf("github: %+v %v", rel, err)
	}

	custom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.4.0","url":"http://example.com/dl"}`))
	}))
	defer custom.Close()
	rel, err = fetchRelease(context.Background(), custom.URL, custom.Client())
	if err != nil || rel.Version != "0.4.0" || rel.URL != "http://example.com/dl" {
		t.Fatalf("custom: %+v %v", rel, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.0.0","url":"file:///etc/passwd"}`))
	}))
	defer bad.Close()
	if _, err := fetchRelease(context.Background(), bad.URL, bad.Client()); err == nil {
		t.Fatal("non-http update URL accepted")
	}
}
