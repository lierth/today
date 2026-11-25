package keychain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A stub `security` that records its commands, so the quoting and the "no
// password in argv" rule are testable without touching the real keychain.
func stub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "security")
	body := `#!/bin/sh
printf '%s\n' "$@" >> ` + filepath.Join(dir, "args") + `
cat >> ` + filepath.Join(dir, "stdin") + `
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	Command = script
	t.Cleanup(func() { Command = "/usr/bin/security" })
	return dir
}

func TestAddScriptQuotesEverything(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain123", `add-generic-password -U -s 'com.lierth.today' -a 'login' -w 'plain123'` + "\n"},
		{"with space", `-w 'with space'`},
		{"a$b#c;d", `-w 'a$b#c;d'`},
		{"it's", `-w 'it'\''s'`},
	}
	for _, c := range cases {
		got := AddScript(Service, Account, c.in)
		if !strings.Contains(got, c.want) {
			t.Errorf("%q: %q missing in %q", c.in, c.want, got)
		}
	}
}

func TestSetKeepsPasswordOutOfArgv(t *testing.T) {
	dir := stub(t)
	const pw = "s3cr3t-value"
	if err := Set(context.Background(), pw); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), pw) {
		t.Fatalf("password reached argv: %s", args)
	}
	if !strings.Contains(string(args), "-i") {
		t.Fatalf("-i missing: %s", args)
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdin), pw) {
		t.Fatalf("password missing from stdin: %s", stdin)
	}
}

func TestGetTrimsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "security")
	// find-generic-password prints the password on stdout with a newline.
	body := "#!/bin/sh\nprintf 'abc123\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	Command = script
	t.Cleanup(func() { Command = "/usr/bin/security" })
	pw, err := Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pw != "abc123" {
		t.Fatalf("got %q", pw)
	}
}

func TestGetMissingIsErrMissing(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "security")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 44\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	Command = script
	t.Cleanup(func() { Command = "/usr/bin/security" })
	if _, err := Get(context.Background()); err != ErrMissing {
		t.Fatalf("got %v", err)
	}
}

func TestGetDoesNotRetryDeniedAccess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "security")
	calls := filepath.Join(dir, "calls")
	body := "#!/bin/sh\nprintf '%s\n' call >> " + ShellQuote(calls) + "\nexit 36\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	Command = script
	t.Cleanup(func() { Command = "/usr/bin/security" })
	if _, err := Get(context.Background()); err != ErrMissing {
		t.Fatalf("got %v", err)
	}
	log, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(log) != "call\n" {
		t.Fatalf("unexpected retry: %s", log)
	}
}

func TestDeleteUsesCurrentService(t *testing.T) {
	dir := stub(t)
	if err := Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	want := "delete-generic-password\n-s\n" + Service + "\n-a\n" + Account + "\n"
	if string(args) != want {
		t.Fatalf("unexpected command: %s", args)
	}
}
