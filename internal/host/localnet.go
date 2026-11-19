// Local network privacy (macOS 15 and later).
//
// Apple's TN3179: an outgoing TCP connection to a local network address is
// what makes the system record and ask for local network access; merely
// listening does not. The app serves other devices, but it never dials one, so
// without an explicit probe macOS never asks and the phone or the board just
// times out. This file owns the probe, the status mapping and the settings
// deep link; host.go only decides when to call them.
package host

import (
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// LocalNetworkState is what the menu and the settings page show.
type LocalNetworkState string

const (
	// LocalNetworkOK: the loopback-to-LAN self-connection worked.
	LocalNetworkOK LocalNetworkState = "ok"
	// LocalNetworkDenied: the OS refused the connection, i.e. the person (or
	// a previous build) said no.
	LocalNetworkDenied LocalNetworkState = "denied"
	// LocalNetworkUnknown: neither proven nor refused — no LAN interface, a
	// timeout, or a Mac where the feature does not exist.
	LocalNetworkUnknown LocalNetworkState = "unknown"
)

// localNetworkURL opens System Settings -> Privacy & Security, where the
// "Local Network" list lives. Verified on macOS 26.2 (27.2.1): the pane is the
// SecurityPrivacyExtension (legacy bundle id com.apple.preference.security,
// allowsXAppleSystemPreferencesURLScheme = true), and it exposes 26 Privacy_*
// anchors — Privacy_Accessibility, Privacy_Camera, … — but no
// Privacy_LocalNetwork one. That anchor is therefore dropped: it would land on
// the pane's default page or be ignored. The pane root always opens.
const localNetworkURL = "x-apple.systempreferences:com.apple.preference.security"

// LocalNetworkNeedsAttention: the menu and the settings page share one rule.
// Unknown counts as needing attention because a Mac with no LAN interface
// still cannot serve a board, and silence would look like "all good". The empty
// state (no probe finished yet, e.g. the alert is still on screen) counts as
// unknown on purpose: the first run should offer the button, not hide it.
func LocalNetworkNeedsAttention(s LocalNetworkState) bool {
	return s != LocalNetworkOK
}

// MapLocalNetworkError turns the error of a TCP self-connection into a state.
// It is pure so the table can be tested without a Mac's privacy database:
//
//	ERR_HOSTUNREACH / EHOSTUNREACH   -> denied (the kernel refuses locally)
//	ENETUNREACH / ENETDOWN           -> unknown (no route: Wi-Fi off, no LAN)
//	ECONNREFUSED                     -> ok (the packet left; nothing listening)
//	timeout / deadline exceeded      -> unknown (the alert may be unanswered)
//	nil                              -> ok
func MapLocalNetworkError(err error) LocalNetworkState {
	if err == nil {
		return LocalNetworkOK
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EHOSTUNREACH, syscall.EHOSTDOWN, syscall.EACCES, syscall.EPERM:
			return LocalNetworkDenied
		case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED:
			return LocalNetworkOK
		case syscall.ENETUNREACH, syscall.ENETDOWN, syscall.EADDRNOTAVAIL, syscall.EAFNOSUPPORT:
			return LocalNetworkUnknown
		}
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, os.ErrNotExist) {
		return LocalNetworkUnknown
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return LocalNetworkUnknown
	}
	return LocalNetworkUnknown
}

// LocalNetworkTarget picks the address the probe dials: the machine's own
// private IPv4 address. Dialing it leaves the host and comes back, which is
// exactly the "outgoing connection to a local network address" that triggers
// the privacy check. An empty result means there is no LAN to test.
func LocalNetworkTarget() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil || !ip4.IsPrivate() {
			continue
		}
		return ip4.String()
	}
	return ""
}

// TriggerLocalNetwork asks the system to record or re-ask for local network
// access, using the caller's port (any port works: the packet is what counts,
// and our own healthz answers it once access is allowed). The probe waits for
// the answer, so a first run blocks until the person dismisses the alert.
func TriggerLocalNetwork(port string, timeout time.Duration) (LocalNetworkState, string) {
	if os.Getenv("TODAY_NO_LOCAL_NETWORK_PROBE") == "1" {
		return LocalNetworkOK, "probe disabled"
	}
	target := LocalNetworkTarget()
	if target == "" {
		return LocalNetworkUnknown, "no private IPv4 interface"
	}
	if port == "" {
		port = "80"
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", net.JoinHostPort(target, port))
	if err == nil {
		_ = conn.Close()
		return LocalNetworkOK, target
	}
	return MapLocalNetworkError(err), target
}

// LocalNetworkExplanation is the one-line status the settings page and the
// menu share, kept out of the UI so both word it identically.
func LocalNetworkExplanation(s LocalNetworkState, target string) string {
	switch s {
	case LocalNetworkOK:
		return ""
	case LocalNetworkDenied:
		if target != "" {
			return "macOS 拒绝了本地网络访问（" + target + "），手机和显示屏连不进来。"
		}
		return "macOS 拒绝了本地网络访问，手机和显示屏连不进来。"
	default:
		return "还没有确认本地网络权限：允许后手机和显示屏才能连进来。"
	}
}

// AdhocSigned reports whether the running bundle is ad-hoc signed. TCC keys a
// permission to the code signature, so every rebuild of an ad-hoc bundle can
// look like a different program and ask again; the settings page says so.
func AdhocSigned() bool {
	if strings.TrimSpace(os.Getenv("TODAY_BUNDLE_ADHOC")) == "1" {
		return true
	}
	return bundleIsAdhoc()
}
