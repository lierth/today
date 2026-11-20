package host

import "github.com/egoist/mygo"

// The settings window is a real window, not a page in the browser: it writes
// through the same bindings the panel uses, so a change lands in the tray and
// on the boards without a session cookie or a reload.
//
// It is frameless with the traffic lights kept (TitleBarHiddenInset): the
// window draws its own header where a toolbar would be, and the page avoids the
// lights with the --mygo-titlebar-* variables mygo sets. A standard title bar
// would put a grey strip above the paper background and break the one-surface
// look.
func (h *Host) openSettings(tab string) {
	url := "/settings"
	if tab != "" {
		url += "?tab=" + tab
	}
	if h.settingsWindow != nil {
		// Already open: mygo cannot navigate an existing window, so replace it.
		// The window is stateless — the tab is in the URL and the data comes
		// from bindings — so reopening loses nothing.
		h.settingsWindow.Close()
		h.settingsWindow = nil
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           "Today 设置",
		URL:             url,
		Width:           760,
		Height:          580,
		MinWidth:        680,
		MinHeight:       460,
		TitleBarStyle:   mygo.TitleBarHiddenInset,
		Vibrancy:        mygo.VibrancyWindow,
		BackgroundColor: "light-dark(#f4f4f6, #1a1a1e)",
		AutoHideMenuBar: true,
		StateKey:        "settings",
	})
	// The callback fires after a slow close; only clear the field when it
	// still points at this window, or a fast reopen would be forgotten.
	win.OnClosed(func() {
		if h.settingsWindow == win {
			h.settingsWindow = nil
		}
	})
	h.settingsWindow = win
}
