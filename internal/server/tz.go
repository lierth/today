package server

import (
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

// zoneTabPaths are where macOS and common Linux distributions list IANA names.
var zoneTabPaths = []string{
	"/usr/share/zoneinfo/zone.tab",
	"/var/db/timezone/zoneinfo/zone.tab",
}

// systemTimezone returns the IANA name the OS is configured with, or "" when
// the machine uses a bare UTC clock without a zoneinfo link.
func systemTimezone() string {
	for _, link := range []string{"/etc/localtime", "/var/db/timezone/localtime"} {
		target, err := os.Readlink(link)
		if err != nil {
			continue
		}
		for _, prefix := range []string{
			"/var/db/timezone/zoneinfo/",
			"/usr/share/zoneinfo/",
			"../usr/share/zoneinfo/",
		} {
			if i := strings.Index(target, prefix); i >= 0 {
				name := target[i+len(prefix):]
				if _, err := loadLocation(name); err == nil {
					return name
				}
			}
		}
	}
	if _, err := loadLocation("UTC"); err == nil {
		return "UTC"
	}
	return ""
}

// zoneChoices lists IANA names for the settings datalist. zone.tab is the
// authoritative list; the curated fallback keeps the page usable on systems
// without it. The current value is always included so an existing "UTC" or a
// hand-typed name still shows up.
func zoneChoices(current string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || name == "Local" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add(current)
	for _, path := range zoneTabPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) >= 3 {
				add(strings.TrimSpace(fields[2]))
			}
		}
		break
	}
	if len(out) <= 1 {
		for _, name := range []string{
			"Asia/Shanghai", "Asia/Tokyo", "Asia/Singapore", "Asia/Hong_Kong",
			"Asia/Taipei", "Asia/Seoul", "Asia/Kolkata", "Asia/Dubai",
			"Europe/London", "Europe/Paris", "Europe/Berlin", "Europe/Madrid",
			"Europe/Moscow", "America/New_York", "America/Chicago",
			"America/Denver", "America/Los_Angeles", "America/Sao_Paulo",
			"Australia/Sydney", "Pacific/Auckland", "UTC",
		} {
			add(name)
		}
	}
	sort.Strings(out)
	return out
}

// displayTimezone is what the settings field shows: an explicit IANA name, or
// the system zone when the stored value is empty or the legacy "Local".
func displayTimezone(stored string) string {
	if stored == "" || stored == "Local" {
		return systemTimezone()
	}
	return stored
}

func loopbackAddr(addr string) string {
	_, port, err := net.SplitHostPort(NormalizeAddr(addr))
	if err != nil || port == "" {
		return "127.0.0.1:8787"
	}
	return net.JoinHostPort("127.0.0.1", port)
}

// lanBase returns "http://<private-ipv4>:<port>" for the setup commands, or the
// loopback base when no private interface exists (offline laptop).
func (s *Server) lanBase() string {
	port := s.Port()
	if port == "" {
		return "http://" + loopbackAddr(s.Addr())
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || !ip4.IsPrivate() {
				continue
			}
			return "http://" + net.JoinHostPort(ip4.String(), port)
		}
	}
	return "http://" + loopbackAddr(s.Addr())
}

// loadLocation is a seam for tests: the real implementation is
// time.LoadLocation, which reads the OS zoneinfo database.
var loadLocation = time.LoadLocation
