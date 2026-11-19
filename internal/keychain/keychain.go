// Package keychain keeps the login password in the macOS login keychain.
//
// The database stores only an Argon2id hash: the file is backed up, synced and
// occasionally mailed around, while Funnel puts the web page on the public
// internet. The plaintext therefore lives in the keychain, where the system
// protects it and the tray can copy it back out.
//
// Nothing here uses cgo: /usr/bin/security is the system client.
package keychain

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Service is the bundle id the item is filed under; Keychain Access shows it
// as the item's name when the person searches for Today.
const Service = "com.lierth.today"

// Account is the account field of the item.
const Account = "login"

// Command overrides the security binary; tests point it at a stub script.
var Command = "/usr/bin/security"

// ErrMissing means there is no item (or it cannot be read) — not a failure
// worth logging, just a disabled menu item.
var ErrMissing = errors.New("keychain: no stored password")

// ShellQuote wraps one word for the `security -i` parser. That parser honours
// single quotes and backslash escapes; single quotes keep $ # ; and spaces
// literal, and a literal quote is written as '\” (verified against
// /usr/bin/security on macOS 26).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// AddScript builds the `security -i` input. The password stays out of argv:
// `security` reads whole commands from stdin (man security: "-i  Run security
// in interactive mode"), and an argument is readable by every `ps` on the
// machine — the very thing `security` warns about for -w.
func AddScript(service, account, pw string) string {
	return fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n",
		ShellQuote(service), ShellQuote(account), ShellQuote(pw))
}

// Set stores or replaces the password.
func Set(ctx context.Context, pw string) error {
	cmd := exec.CommandContext(ctx, Command, "-i")
	cmd.Stdin = strings.NewReader(AddScript(Service, Account, pw))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain: add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Get returns the stored password, or ErrMissing.
func Get(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, Command,
		"find-generic-password", "-s", Service, "-a", Account, "-w").Output()
	if err != nil {
		return "", ErrMissing
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Delete removes the item; a missing item is success.
func Delete(ctx context.Context) error {
	_ = exec.CommandContext(ctx, Command,
		"delete-generic-password", "-s", Service, "-a", Account).Run()
	return nil
}
