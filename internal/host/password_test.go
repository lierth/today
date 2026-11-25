package host

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestUnavailablePasswordDoesNotWidenMenu(t *testing.T) {
	h, _, _ := newPanelEnv(t)
	keychain.Command = "/bin/false"
	t.Cleanup(func() { keychain.Command = "/usr/bin/security" })
	menu := h.buildMenu()
	password := menu.ItemByID("copy-password")
	if password == nil || password.Label != "复制登录口令" || !password.Disabled || password.ToolTip == "" {
		t.Fatalf("unavailable password item: %+v", password)
	}
	for _, item := range menu.Items() {
		if item.Hidden {
			continue
		}
		if item == password {
			t.Fatal("password belongs in the access submenu")
		}
		if utf8.RuneCountInString(item.Label) > 16 || utf8.RuneCountInString(item.Subtitle) > 18 {
			t.Fatalf("top-level menu text is too long: %q / %q", item.Label, item.Subtitle)
		}
	}
}

func TestRemoteAddressDoesNotWidenMenu(t *testing.T) {
	h, _, st := newPanelEnv(t)
	keychain.Command = "/bin/false"
	t.Cleanup(func() { keychain.Command = "/usr/bin/security" })
	address := "https://" + strings.Repeat("long-host-", 12) + "example.ts.net"
	if err := st.PutSettings(context.Background(), map[string]string{"funnel_host": address}); err != nil {
		t.Fatal(err)
	}
	item := h.buildMenu().ItemByID("copy-remote-address")
	if item == nil || item.Badge != "" || item.ToolTip != address {
		t.Fatalf("the full address belongs on hover, not in a menu column: %+v", item)
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
