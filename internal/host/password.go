// Password state for the tray copy button.
//
// The database stores only an Argon2id hash; the plaintext lives in the login
// keychain (internal/keychain). Keeping the two in step matters: a stale
// keychain copy would put a wrong password on the clipboard, so the button
// verifies against the hash before it copies and greys out when they differ.
package host

import (
	"context"
	"errors"

	"today/internal/keychain"
	"today/internal/server"
)

// ErrPasswordStale reports that the stored copy was replaced elsewhere; the
// tray shows the disabled item and the settings page offers a reset.
var ErrPasswordStale = errors.New("keychain password does not match the database")

// CopyablePassword decides whether the tray may offer the password. It checks
// the hash, not just presence: a keychain item from an older install must not
// put a wrong password on the clipboard and send the person to a failed login.
func CopyablePassword(ctx context.Context, srv *server.Server) (string, error) {
	pw, err := keychain.Get(ctx)
	if err != nil || pw == "" {
		return "", ErrPasswordStale
	}
	ok, err := srv.CheckPassword(ctx, pw)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrPasswordStale
	}
	return pw, nil
}

// clipboardClearDue is the pure half of the 60 s clipboard wipe: it says
// whether the clipboard still holds exactly what we put there. A person who
// copied something else in the meantime must keep it.
func clipboardClearDue(current, copied string) bool {
	return copied != "" && current == copied
}
