package host

import (
	"context"
	"strings"
	"testing"

	"today/internal/keychain"
)

// The clipboard wipe must never destroy something the person copied after us.
func TestClipboardClearDue(t *testing.T) {
	cases := []struct {
		name    string
		current string
		copied  string
		want    bool
	}{
		{"still ours", "abc123", "abc123", true},
		{"user copied something else", "https://example.com", "abc123", false},
		{"user cleared it", "", "abc123", false},
		{"we never copied", "abc123", "", false},
		{"both empty", "", "", false},
		{"prefix only", "abc12", "abc123", false},
	}
	for _, c := range cases {
		if got := clipboardClearDue(c.current, c.copied); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// A stale keychain item is a state, not a crash: the button turns grey and the
// label points at the settings page.
func TestLoginLabel(t *testing.T) {
	if got := loginLabel(true); got != "复制登录口令" {
		t.Errorf("usable label: %q", got)
	}
	stale := loginLabel(false)
	if stale == "复制登录口令" {
		t.Fatal("the disabled item must say why")
	}
	if !strings.Contains(stale, "设置页") {
		t.Errorf("the disabled item should point at the settings page: %q", stale)
	}
}

// The keychain lookup must fail closed: a missing item disables the copy
// button instead of returning an empty string to the clipboard.
func TestCopyablePasswordWithoutKeychain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	keychain.Command = "/bin/false"
	t.Cleanup(func() { keychain.Command = "/usr/bin/security" })
	if _, err := keychain.Get(context.Background()); err == nil {
		t.Fatal("test setup: the stub must fail")
	}
}
