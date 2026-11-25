// Package boardlink discovers Today displays on the directly attached LAN.
package boardlink

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"sync"
	"time"
)

const Port = 8789

var hardwareID = regexp.MustCompile(`^today-[0-9a-f]{12}$`)

func ValidID(id string) bool { return hardwareID.MatchString(id) }

type Board struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Pairable bool   `json:"pairable"`
	Address  string `json:"-"`
	LocalIP  string `json:"-"`
}

type announcement struct {
	Service  string `json:"service"`
	Version  int    `json:"version"`
	Nonce    string `json:"nonce"`
	ID       string `json:"id"`
	Pairable bool   `json:"pairable"`
}

// Scan uses one socket per LAN address so VPN routes cannot choose the server
// address that is written into a display. The nonce rejects old announcements.
func Scan(ctx context.Context) ([]Board, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	type result struct {
		boards []Board
		err    error
	}
	results := make(chan result, len(interfaces)*4)
	var workers sync.WaitGroup
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok || network.IP.To4() == nil || !network.IP.IsPrivate() {
				continue
			}
			ip := network.IP.To4()
			mask := network.Mask
			if len(mask) != net.IPv4len {
				continue
			}
			broadcast := net.IPv4(ip[0]|^mask[0], ip[1]|^mask[1], ip[2]|^mask[2], ip[3]|^mask[3])
			workers.Add(1)
			go func() {
				defer workers.Done()
				boards, err := scanAddress(ctx, ip, &net.UDPAddr{IP: broadcast, Port: Port})
				results <- result{boards, err}
			}()
		}
	}
	go func() { workers.Wait(); close(results) }()
	found := map[string]Board{}
	var lastError error
	usable := false
	for result := range results {
		if result.err != nil {
			lastError = result.err
		} else {
			usable = true
		}
		for _, board := range result.boards {
			found[board.ID] = board
		}
	}
	if !usable && len(found) == 0 {
		if lastError != nil {
			return nil, fmt.Errorf("局域网搜索失败：%w", lastError)
		}
		return nil, errors.New("请先让 Mac 接入局域网")
	}
	boards := make([]Board, 0, len(found))
	for _, board := range found {
		boards = append(boards, board)
	}
	sort.Slice(boards, func(i, j int) bool { return boards[i].ID < boards[j].ID })
	return boards, nil
}

func scanAddress(ctx context.Context, localIP net.IP, target *net.UDPAddr) ([]Board, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	nonce := hex.EncodeToString(random[:])
	query, _ := json.Marshal(announcement{Service: "today", Version: 1, Nonce: nonce})
	found := map[string]Board{}
	var buffer [1024]byte
	for ctx.Err() == nil {
		if _, err := conn.WriteToUDP(query, target); err != nil {
			return nil, err
		}
		_ = conn.SetReadDeadline(time.Now().Add(650 * time.Millisecond))
		for {
			n, source, err := conn.ReadFromUDP(buffer[:])
			if err != nil {
				break
			}
			board, ok := decodeAnnouncement(buffer[:n], nonce, source.IP, localIP)
			if ok {
				found[board.ID] = board
			}
		}
	}
	boards := make([]Board, 0, len(found))
	for _, board := range found {
		boards = append(boards, board)
	}
	return boards, nil
}

func decodeAnnouncement(data []byte, nonce string, remote, local net.IP) (Board, bool) {
	var reply announcement
	if json.Unmarshal(data, &reply) != nil || reply.Service != "today" || reply.Version != 1 ||
		reply.Nonce != nonce || !ValidID(reply.ID) || remote.To4() == nil || !remote.IsPrivate() {
		return Board{}, false
	}
	return Board{ID: reply.ID, Name: "显示屏 · " + reply.ID[len(reply.ID)-6:], Pairable: reply.Pairable,
		Address: remote.String(), LocalIP: local.String()}, true
}
