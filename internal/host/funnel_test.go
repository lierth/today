package host

import "testing"

func TestDiagnoseFunnel(t *testing.T) {
	const run = `{"BackendState":"Running","Self":{"DNSName":"box.tail.ts.net","CapMap":{"https":null,"funnel":null}}}`
	cases := []struct {
		name string
		in   FunnelInput
		want FunnelState
		url  string
	}{
		{
			name: "no binary",
			in:   FunnelInput{},
			want: FunnelMissing,
			url:  downloadURL,
		},
		{
			name: "app store sandbox cannot funnel",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/Applications/Tailscale.app/Contents/MacOS/Tailscale",
			},
			want: FunnelMissing,
			url:  openSourceURL,
		},
		{
			name: "status unreadable",
			in:   FunnelInput{BinaryFound: true, BinaryPath: "/opt/homebrew/bin/tailscale"},
			want: FunnelLoggedOut,
			url:  loginFallback,
		},
		{
			name: "needs login with auth url",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/opt/homebrew/bin/tailscale",
				StatusJSON:  []byte(`{"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/abc"}`),
			},
			want: FunnelLoggedOut,
			url:  "https://login.tailscale.com/a/abc",
		},
		{
			name: "stopped means log in again",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/opt/homebrew/bin/tailscale",
				StatusJSON:  []byte(`{"BackendState":"Stopped"}`),
			},
			want: FunnelLoggedOut,
			url:  loginFallback,
		},
		{
			name: "starting",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/opt/homebrew/bin/tailscale",
				StatusJSON:  []byte(`{"BackendState":"Starting"}`),
			},
			want: FunnelConnecting,
		},
		{
			name: "no https capability",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/opt/homebrew/bin/tailscale",
				StatusJSON:  []byte(`{"BackendState":"Running","Self":{"CapMap":{"funnel":null}}}`),
			},
			want: FunnelNeedsSetup,
			url:  httpsSetupURL,
		},
		{
			name: "no funnel attribute",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/opt/homebrew/bin/tailscale",
				StatusJSON:  []byte(`{"BackendState":"Running","Self":{"CapMap":{"https":null}}}`),
			},
			want: FunnelNeedsSetup,
			url:  funnelAttrURL,
		},
		{
			name: "capabilities but not on yet",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/opt/homebrew/bin/tailscale",
				StatusJSON:  []byte(run),
			},
			want: FunnelConnecting,
		},
		{
			name: "local backend port 8787 is valid",
			in: FunnelInput{
				BinaryFound: true,
				BinaryPath:  "/opt/homebrew/bin/tailscale",
				StatusJSON:  []byte(run),
			},
			want: FunnelConnecting,
		},
		{
			name: "on",
			in: FunnelInput{
				BinaryFound:  true,
				BinaryPath:   "/opt/homebrew/bin/tailscale",
				StatusJSON:   []byte(run),
				FunnelOutput: "# Funnel on:\n#     - https://box.tail.ts.net\n",
			},
			want: FunnelOn,
			url:  "https://box.tail.ts.net",
		},
		{
			name: "funnel error asks for login",
			in: FunnelInput{
				BinaryFound:  true,
				BinaryPath:   "/opt/homebrew/bin/tailscale",
				StatusJSON:   []byte(run),
				FunnelOutput: "visit https://login.tailscale.com/a/xyz to authenticate",
				FunnelErr:    true,
			},
			want: FunnelLoggedOut,
			url:  "https://login.tailscale.com/a/xyz",
		},
		{
			name: "funnel error needs https",
			in: FunnelInput{
				BinaryFound:  true,
				BinaryPath:   "/opt/homebrew/bin/tailscale",
				StatusJSON:   []byte(run),
				FunnelOutput: "Funnel not available; HTTPS must be enabled.",
				FunnelErr:    true,
			},
			want: FunnelNeedsSetup,
			url:  httpsSetupURL,
		},
		{
			name: "funnel error needs attribute",
			in: FunnelInput{
				BinaryFound:  true,
				BinaryPath:   "/opt/homebrew/bin/tailscale",
				StatusJSON:   []byte(run),
				FunnelOutput: `Funnel not available; "funnel" node attribute not set.`,
				FunnelErr:    true,
			},
			want: FunnelNeedsSetup,
			url:  funnelAttrURL,
		},
		{
			name: "permission denied",
			in: FunnelInput{
				BinaryFound:  true,
				BinaryPath:   "/opt/homebrew/bin/tailscale",
				StatusJSON:   []byte(run),
				FunnelOutput: "permission denied while connecting to tailscaled",
				FunnelErr:    true,
			},
			want: FunnelNeedsSetup,
			url:  openSourceURL,
		},
		{
			name: "port not allowed",
			in: FunnelInput{
				BinaryFound:  true,
				BinaryPath:   "/opt/homebrew/bin/tailscale",
				StatusJSON:   []byte(run),
				FunnelOutput: "port 8787 is not allowed for funnel",
				FunnelErr:    true,
			},
			want: FunnelNeedsSetup,
			url:  portForbiddenDC,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DiagnoseFunnel(c.in)
			if got.State != c.want {
				t.Fatalf("state: got %q want %q (%s)", got.State, c.want, got.Detail)
			}
			if c.url != "" && got.ActionURL != c.url {
				t.Fatalf("url: got %q want %q", got.ActionURL, c.url)
			}
			if c.url == "" && got.State != FunnelConnecting && got.ActionURL != "" {
				t.Fatalf("unexpected url %q", got.ActionURL)
			}
		})
	}
}

func TestFunnelStateNeverEmptyOnError(t *testing.T) {
	// A malformed status and a failing funnel command must still resolve to a
	// state the menu can render, never the zero value.
	got := DiagnoseFunnel(FunnelInput{
		BinaryFound:  true,
		BinaryPath:   "/opt/homebrew/bin/tailscale",
		StatusJSON:   []byte("{"),
		FunnelOutput: "boom",
		FunnelErr:    true,
	})
	if got.State == "" {
		t.Fatal("empty funnel state")
	}
}
