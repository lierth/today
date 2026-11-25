package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"today/internal/boardlink"
	"today/internal/store"
)

var pairCodePattern = regexp.MustCompile(`^[0-9]{6}$`)
var pairTicketPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var localHostnamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}\.local$`)

// DiscoverBoards is host-only: browser visitors cannot scan the person's LAN.
func (s *Server) DiscoverBoards(ctx context.Context) ([]boardlink.Board, error) {
	scan := s.boardScan
	if scan == nil {
		scan = boardlink.Scan
	}
	boards, err := scan(ctx)
	if err != nil {
		return nil, err
	}
	s.boardMu.Lock()
	defer s.boardMu.Unlock()
	s.boards = make(map[string]boardlink.Board, len(boards))
	for _, board := range boards {
		s.boards[board.ID] = board
	}
	return boards, nil
}

// PairBoard first proves that the person can read the physical display. Only
// then is a token created; a wrong code never leaves a phantom device row.
func (s *Server) PairBoard(ctx context.Context, id, code, name string) (store.Device, error) {
	if !boardlink.ValidID(id) || !pairCodePattern.MatchString(code) {
		return store.Device{}, errors.New("请输入屏幕上的 6 位配对码")
	}
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	s.boardMu.Lock()
	board, ok := s.boards[id]
	s.boardMu.Unlock()
	if !ok {
		return store.Device{}, errors.New("请先刷新设备列表后重试")
	}
	if !board.Pairable {
		return store.Device{}, errors.New("请按住板子的 BOOT 键 3 秒，打开配对码")
	}
	client := s.boardHTTP
	if client == nil {
		dialer := &net.Dialer{Timeout: 4 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(board.LocalIP)}}
		transport := &http.Transport{DialContext: dialer.DialContext}
		defer transport.CloseIdleConnections()
		client = &http.Client{Timeout: 5 * time.Second, Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	claim := newDeviceToken()
	// A cached address is only a hint; every pairing verifies the display ID and
	// its current on-screen code before writing credentials.
	verification, err := postBoard(ctx, client, board, "/pair/verify", map[string]string{"id": id, "code": code, "claim": claim})
	if err != nil {
		return store.Device{}, err
	}
	if verification.ID != id || !pairTicketPattern.MatchString(verification.Ticket) {
		return store.Device{}, errors.New("显示屏返回的身份无效，请重新搜索")
	}
	name = strings.TrimSpace(name)
	if existing, err := s.st.Device(ctx, id); err == nil && name == "" {
		name = existing.Name
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Device{}, err
	}
	if name == "" {
		name = board.Name
	}
	if len(name) > 120 {
		return store.Device{}, errors.New("设备名称过长")
	}
	token := newDeviceToken()
	device, err := s.st.PairDevice(ctx, id, name, tokenHash(token))
	if err != nil {
		return store.Device{}, err
	}
	_, port, err := net.SplitHostPort(s.Addr())
	if err != nil {
		return store.Device{}, err
	}
	host := board.LocalIP
	if runtime.GOOS == "darwin" {
		if hostname, err := os.Hostname(); err == nil {
			hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
			if localHostnamePattern.MatchString(hostname) {
				host = hostname
			}
		}
	}
	committed, err := postBoard(ctx, client, board, "/pair", map[string]string{
		"id": id, "ticket": verification.Ticket, "claim": claim,
		"server": "http://" + net.JoinHostPort(host, port), "token": token,
	})
	if err != nil {
		return store.Device{}, err
	}
	if committed.ID != id || !committed.Paired {
		return store.Device{}, errors.New("显示屏尚未确认配对，请重试")
	}
	s.boardMu.Lock()
	delete(s.boards, id)
	s.boardMu.Unlock()
	return device, nil
}

type boardReply struct {
	ID     string `json:"id"`
	Ticket string `json:"ticket"`
	Paired bool   `json:"paired"`
}

func postBoard(ctx context.Context, client *http.Client, board boardlink.Board, path string, payload map[string]string) (boardReply, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return boardReply{}, err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", "http://"+net.JoinHostPort(board.Address, fmt.Sprint(boardlink.Port))+path, bytes.NewReader(body))
	if err != nil {
		return boardReply{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return boardReply{}, errors.New("无法连接显示屏，请确认仍在同一局域网后重试")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusForbidden:
		return boardReply{}, errors.New("配对码不正确或已过期，请查看板子屏幕")
	case http.StatusConflict:
		return boardReply{}, errors.New("配对已关闭，请按住板子的 BOOT 键 3 秒重新打开")
	case http.StatusTooManyRequests:
		return boardReply{}, errors.New("尝试过于频繁，请等 30 秒后重试")
	case http.StatusLocked:
		return boardReply{}, errors.New("显示屏正在处理另一轮配对，请等 30 秒后重试")
	case http.StatusOK:
	default:
		return boardReply{}, errors.New("显示屏未能保存配对信息，请重试")
	}
	var result boardReply
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&result); err != nil {
		return boardReply{}, errors.New("显示屏返回的配对结果无效")
	}
	return result, nil
}
