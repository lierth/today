package host

import (
	"time"

	"today/internal/frame"
)

// lunarLabel is the Chinese lunar date the menu header shows. It goes through
// frame so the menu and the panel never disagree about the day.
func lunarLabel(t time.Time) string {
	return frame.LunarFor(t).Label
}
