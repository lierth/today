package host

import (
	"encoding/json"
	"regexp"
	"strings"
)

// FunnelState is the small state machine behind the "外网访问" item. The
// values are written to settings so the settings page can show the same
// wording without duplicating the logic.
type FunnelState string

const (
	FunnelMissing    FunnelState = "missing"
	FunnelLoggedOut  FunnelState = "logged_out"
	FunnelNeedsSetup FunnelState = "needs_funnel"
	FunnelConnecting FunnelState = "connecting"
	FunnelOn         FunnelState = "on"
)

// FunnelInput is everything the decision needs, kept as raw command output so
// the function is pure and testable without a Mac or a tailnet.
type FunnelInput struct {
	BinaryFound  bool
	BinaryPath   string
	StatusJSON   []byte
	FunnelOutput string // "funnel status" or the failing "funnel --bg" output
	FunnelErr    bool
}

// FunnelVerdict is the single decision the menu, the tray icon and the
// settings page all read.
type FunnelVerdict struct {
	State     FunnelState
	ActionURL string // what a click should open; empty when there is nothing to do
	Detail    string // one-line explanation for the settings page
}

type tailscaleStatus struct {
	BackendState string `json:"BackendState"`
	AuthURL      string `json:"AuthURL"`
	Self         *struct {
		DNSName string                     `json:"DNSName"`
		CapMap  map[string]json.RawMessage `json:"CapMap"`
	} `json:"Self"`
}

var (
	funnelStatusURL = regexp.MustCompile(`https://[A-Za-z0-9.-]+\.ts\.net(?::[0-9]+)?`)
	loginURL        = regexp.MustCompile(`https://login\.tailscale\.com/[A-Za-z0-9/_.?=&%-]+`)
)

const (
	downloadURL     = "https://tailscale.com/download/mac"
	httpsSetupURL   = "https://tailscale.com/s/https"
	funnelAttrURL   = "https://tailscale.com/s/no-funnel"
	openSourceURL   = "https://tailscale.com/kb/1065/macos-variants"
	loginFallback   = "https://login.tailscale.com/"
	portForbiddenDC = "https://tailscale.com/kb/1223/funnel"
)

// DiagnoseFunnel classifies one snapshot. It never runs a command.
func DiagnoseFunnel(in FunnelInput) FunnelVerdict {
	if !in.BinaryFound {
		return FunnelVerdict{
			State:     FunnelMissing,
			ActionURL: downloadURL,
			Detail:    "未找到 Tailscale CLI",
		}
	}
	// The sandboxed App Store/Standalone CLIs cannot run Funnel on macOS;
	// only the open-source tailscaled variant can.
	if strings.Contains(in.BinaryPath, "/Applications/Tailscale.app/") {
		return FunnelVerdict{
			State:     FunnelMissing,
			ActionURL: openSourceURL,
			Detail:    "macOS 上 Funnel 需要开源版 tailscaled（Homebrew），图形版不支持",
		}
	}

	var st tailscaleStatus
	statusErr := len(in.StatusJSON) == 0 || json.Unmarshal(in.StatusJSON, &st) != nil
	if statusErr {
		// No readable status: either the daemon is down or the CLI is not
		// logged in. `tailscale up` is the one action that fixes both.
		return FunnelVerdict{
			State:     FunnelLoggedOut,
			ActionURL: loginFallback,
			Detail:    "读不到 Tailscale 状态，需要登录或启动后台",
		}
	}
	if st.AuthURL != "" {
		return FunnelVerdict{State: FunnelLoggedOut, ActionURL: st.AuthURL, Detail: "等待浏览器授权"}
	}
	switch st.BackendState {
	case "NeedsLogin", "NoState", "Stopped":
		return FunnelVerdict{State: FunnelLoggedOut, ActionURL: loginFallback, Detail: "Tailscale 未登录"}
	case "Starting":
		return FunnelVerdict{State: FunnelConnecting, Detail: "Tailscale 正在启动"}
	case "Running":
		// continue below
	default:
		return FunnelVerdict{State: FunnelConnecting, Detail: "Tailscale 状态：" + st.BackendState}
	}

	caps := map[string]bool{}
	if st.Self != nil {
		for k := range st.Self.CapMap {
			caps[k] = true
		}
	}
	if !caps["https"] {
		return FunnelVerdict{State: FunnelNeedsSetup, ActionURL: httpsSetupURL, Detail: "tailnet 未开启 HTTPS 证书"}
	}
	if !caps["funnel"] {
		return FunnelVerdict{State: FunnelNeedsSetup, ActionURL: funnelAttrURL, Detail: "tailnet 未授予 funnel 节点属性"}
	}

	if m := funnelStatusURL.FindString(in.FunnelOutput); m != "" {
		return FunnelVerdict{State: FunnelOn, ActionURL: m, Detail: "已开启"}
	}
	if in.FunnelErr {
		if m := loginURL.FindString(in.FunnelOutput); m != "" {
			return FunnelVerdict{State: FunnelLoggedOut, ActionURL: m, Detail: "需要重新登录授权"}
		}
		lower := strings.ToLower(in.FunnelOutput)
		switch {
		case strings.Contains(lower, "https must be enabled") || strings.Contains(lower, "https/"):
			return FunnelVerdict{State: FunnelNeedsSetup, ActionURL: httpsSetupURL, Detail: "需要开启 HTTPS"}
		case strings.Contains(lower, "node attribute") || strings.Contains(lower, "no-funnel"):
			return FunnelVerdict{State: FunnelNeedsSetup, ActionURL: funnelAttrURL, Detail: "需要 funnel 节点属性"}
		case strings.Contains(lower, "not allowed for funnel"):
			// CheckFunnelPort judges the public --https port; we always use the
			// default 443, so this means the tailnet restricts public ports.
			return FunnelVerdict{State: FunnelNeedsSetup, ActionURL: portForbiddenDC, Detail: "tailnet 限制了 Funnel 公网端口"}
		case strings.Contains(lower, "permission denied") || strings.Contains(lower, "must be run as root"):
			return FunnelVerdict{State: FunnelNeedsSetup, ActionURL: openSourceURL, Detail: "权限不足：开源版 tailscaled/CLI 需要管理员权限"}
		}
	}
	return FunnelVerdict{State: FunnelConnecting, Detail: "正在等待 Funnel 生效"}
}

// funnelDownloadURL is exported for the menu label; the value is stable.
func funnelDownloadURL() string { return downloadURL }
