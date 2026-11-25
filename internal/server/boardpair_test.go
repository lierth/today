package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"today/internal/boardlink"
)

type boardRoundTrip func(*http.Request) (*http.Response, error)

func (f boardRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBoardPairingRequiresCodeBeforeCreatingDevice(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	const id = "today-aabbccddeeff"
	const ticket = "0123456789abcdef0123456789abcdef"
	e.srv.boardScan = func(context.Context) ([]boardlink.Board, error) {
		return []boardlink.Board{{ID: id, Name: "显示屏 · ddeeff", Pairable: true, Address: "192.168.1.50", LocalIP: "192.168.1.10"}}, nil
	}
	if _, err := e.srv.DiscoverBoards(ctx); err != nil {
		t.Fatal(err)
	}
	commits := 0
	verifiedID := id
	var issuedToken string
	e.srv.boardHTTP = &http.Client{Transport: boardRoundTrip(func(r *http.Request) (*http.Response, error) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.Host != "192.168.1.50:8789" || body["id"] != id {
			t.Fatal("request reached another board")
		}
		status := http.StatusOK
		response := `{"id":"` + verifiedID + `","ticket":"` + ticket + `"}`
		switch r.URL.Path {
		case "/pair/verify":
			if body["code"] != "123456" {
				status = http.StatusForbidden
			}
		case "/pair":
			commits++
			issuedToken = body["token"]
			if body["ticket"] != ticket || !strings.HasPrefix(body["server"], "http://") {
				t.Fatal("pairing proof or server was lost")
			}
			stored, err := e.st.Device(ctx, id)
			if err != nil || stored.TokenHash != tokenHash(issuedToken) {
				t.Fatal("board was given a token the Mac cannot authorize")
			}
			response = `{"id":"` + id + `","paired":true}`
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(response)), Header: http.Header{}}, nil
	})}
	if _, err := e.srv.PairBoard(ctx, id, "000000", "书房"); err == nil {
		t.Fatal("wrong code accepted")
	}
	if devices, _ := e.st.Devices(ctx); len(devices) != 0 || commits != 0 {
		t.Fatal("wrong code created a phantom device")
	}
	verifiedID = "today-000000000000"
	if _, err := e.srv.PairBoard(ctx, id, "123456", "书房"); err == nil {
		t.Fatal("a cached address that now belongs to another display was accepted")
	}
	if devices, _ := e.st.Devices(ctx); len(devices) != 0 || commits != 0 {
		t.Fatal("wrong display identity wrote pairing credentials")
	}
	verifiedID = id
	paired, err := e.srv.PairBoard(ctx, id, "123456", "书房")
	if err != nil || paired.ID != id || paired.Name != "书房" {
		t.Fatalf("pair: %v, %v", paired, err)
	}
	request := httptest.NewRequest("GET", "/v1/devices/"+id+"/frame", nil)
	request.Header.Set("X-Device-Token", issuedToken)
	if _, ok := e.srv.authorizeDevice(request, id); !ok {
		t.Fatal("paired display cannot authenticate")
	}
	request.Header.Set("X-Device-Token", "another-display-token")
	if _, ok := e.srv.authorizeDevice(request, id); ok {
		t.Fatal("another display's token was accepted")
	}
	if _, err := e.srv.PairBoard(ctx, id, "123456", ""); err == nil {
		t.Fatal("a consumed discovery entry was reused")
	}
	if _, err := e.srv.DiscoverBoards(ctx); err != nil {
		t.Fatal(err)
	}
	previousToken := issuedToken
	repaired, err := e.srv.PairBoard(ctx, id, "123456", "")
	if err != nil || repaired.Name != "书房" || !repaired.CreatedAt.Equal(paired.CreatedAt) {
		t.Fatalf("same hardware should keep its name and history: %v", err)
	}
	if devices, _ := e.st.Devices(ctx); len(devices) != 1 {
		t.Fatal("one display became multiple devices")
	}
	request.Header.Set("X-Device-Token", previousToken)
	if _, ok := e.srv.authorizeDevice(request, id); ok {
		t.Fatal("re-pairing restored the previous token")
	}
}
