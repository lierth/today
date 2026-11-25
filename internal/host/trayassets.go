// Menu-bar glyphs, embedded so the toggle and the alert dot are drawn from
// the same artwork as resources/tray.svg. `go run ./resources/gen` writes these
// files; a missing or unreadable image falls back to the hand-drawn glyph in
// drawTrayGlyph, so a broken asset never leaves the menu bar empty.
package host

import (
	_ "embed"
)

//go:embed assets/tray@2x.png
var tray2xPNG []byte

//go:embed assets/tray-alert@2x.png
var trayAlert2xPNG []byte
