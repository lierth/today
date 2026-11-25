package boardlink

import (
	"encoding/json"
	"net"
	"testing"
)

func TestAnnouncementRequiresIdentityAndCurrentSearch(t *testing.T) {
	valid := announcement{Service: "today", Version: 1, Nonce: "current-search", ID: "today-aabbccddeeff", Pairable: true}
	for _, test := range []struct {
		name    string
		mutate  func(*announcement)
		address string
		want    bool
	}{
		{"display", func(*announcement) {}, "192.168.1.50", true},
		{"old search", func(a *announcement) { a.Nonce = "old" }, "192.168.1.50", false},
		{"other protocol", func(a *announcement) { a.Service = "other" }, "192.168.1.50", false},
		{"manual id", func(a *announcement) { a.ID = "dev1" }, "192.168.1.50", false},
		{"public address", func(*announcement) {}, "8.8.8.8", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			reply := valid
			test.mutate(&reply)
			data, _ := json.Marshal(reply)
			board, ok := decodeAnnouncement(data, valid.Nonce, net.ParseIP(test.address), net.ParseIP("192.168.1.10"))
			if ok != test.want {
				t.Fatalf("accepted=%v, want %v", ok, test.want)
			}
			if ok && (board.ID != valid.ID || board.Address != test.address || board.LocalIP != "192.168.1.10") {
				t.Fatal("identity and route were not retained")
			}
			if ok {
				public, _ := json.Marshal(board)
				var fields map[string]any
				_ = json.Unmarshal(public, &fields)
				if fields["Address"] != nil || fields["LocalIP"] != nil {
					t.Fatal("routing internals leaked to the UI")
				}
			}
		})
	}
}
