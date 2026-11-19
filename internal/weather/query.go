package weather

import "strings"

// Source says where a device's location came from, for the settings page.
type Source string

const (
	SourceDeviceCity Source = "device_city"
	SourceGlobalCity Source = "global_city"
	SourceClientIP   Source = "client_ip"
	SourceServerIP   Source = "server_ip"
	SourceNone       Source = "none"
)

// Location is the resolved `q` for one frame request.
type Location struct {
	Q      string
	Source Source
}

// SelectQuery decides the q parameter for one request. It is pure so the
// priority can be tested without a network or a database:
//
//  1. the device's own city,
//  2. the global default city,
//  3. with IP fallback on: the board's public IP when the request came through
//     Funnel (tailscaled sets X-Forwarded-For and we only pass a trusted value
//     in), otherwise auto:ip, which WeatherAPI resolves from the server's own
//     egress address — the same router the board uses when it is on the LAN,
//  4. otherwise no weather.
//
// clientIP must already be the trusted first hop (see server.isFunnelRequest);
// an empty value simply falls through to auto:ip.
func SelectQuery(deviceCity, globalCity string, ipFallback, funnel bool, clientIP string) Location {
	if q := strings.TrimSpace(deviceCity); q != "" {
		return Location{Q: q, Source: SourceDeviceCity}
	}
	if q := strings.TrimSpace(globalCity); q != "" {
		return Location{Q: q, Source: SourceGlobalCity}
	}
	if !ipFallback {
		return Location{Source: SourceNone}
	}
	if funnel {
		if ip := strings.TrimSpace(clientIP); ip != "" {
			return Location{Q: ip, Source: SourceClientIP}
		}
	}
	return Location{Q: "auto:ip", Source: SourceServerIP}
}

// SourceLabel is the settings-page wording for a location.
func SourceLabel(s Source) string {
	switch s {
	case SourceDeviceCity:
		return "已配置"
	case SourceGlobalCity:
		return "默认城市"
	case SourceClientIP:
		return "来自 IP，约城市级"
	case SourceServerIP:
		return "来自 IP（服务端出口）"
	default:
		return "未配置"
	}
}
