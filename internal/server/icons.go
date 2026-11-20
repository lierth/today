package server

import (
	"net/http"

	"today/internal/server/webapp"
)

// loginIcons are the images the inline login page names in its <head>. They
// are served without a session: a browser asks for the favicon before anyone
// has typed the password, and a 302 to /login is not an image.
var loginIcons = []string{"favicon-light.png", "favicon-dark.png", "apple-touch-icon.png"}

// handleIcon answers one of loginIcons from the same bundle the app serves, so
// the tab icon and the touch icon stay the files resources/gen draws.
func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[1:]
	body, ctype, ok := webapp.Icon(name, s.cfg.Dev)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	// The name carries no content hash, so revalidate: the files are a few
	// hundred bytes and the browser asks once per visit.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(body)
}
