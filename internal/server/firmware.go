package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"today/internal/release"
)

// Both OTA application slots in firmware/partitions.csv are 3 MiB.
const maxFirmwareBytes = 3 << 20

func (s *Server) firmwarePath() string {
	if s.cfg.Paths.Firmware != "" {
		return filepath.Join(s.cfg.Paths.Firmware, "firmware.bin")
	}
	if s.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "firmware.bin")
}

func (s *Server) firmwareSlotPath(slot string) string {
	path := s.firmwarePath()
	if path == "" || slot == "" {
		return path // Existing installations use firmware.bin.
	}
	if slot != "a" && slot != "b" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "firmware-"+slot+".bin")
}

// handleFirmware serves the uploaded ESP32 image to an authenticated device.
// The ETag lets a device skip a download it already has.
func (s *Server) handleFirmware(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.authorizeDevice(r, id); !ok {
		http.NotFound(w, r)
		return
	}
	s.firmwareMu.RLock()
	defer s.firmwareMu.RUnlock()
	settings, err := s.st.Settings(r.Context())
	if err != nil {
		http.Error(w, "settings", http.StatusInternalServerError)
		return
	}
	path := s.firmwareSlotPath(settings["firmware_slot"])
	if path == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		http.Error(w, "read", http.StatusInternalServerError)
		return
	}
	etag := `"` + hex.EncodeToString(h.Sum(nil)[:8]) + `"`
	version := settings["firmware_version"]

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Firmware-Version", version)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "seek", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	http.ServeContent(w, r, "firmware.bin", info.ModTime(), f)
}

// SaveFirmware stages a verified image from the updater into the same slot the
// settings page uploads to, without pushing it to any board. The caller has
// already checked it against the release manifest.
func (s *Server) SaveFirmware(path, version string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	return s.StageFirmware(src, version)
}

// StageFirmware writes an ESP32-S3 application image to the inactive slot.
func (s *Server) StageFirmware(src io.Reader, version string) error {
	path := s.firmwarePath()
	if path == "" {
		return fmt.Errorf("上传目录未配置")
	}
	data, err := io.ReadAll(io.LimitReader(src, maxFirmwareBytes+1))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("固件文件为空")
	}
	if len(data) > maxFirmwareBytes {
		return fmt.Errorf("固件超过 %d MB", maxFirmwareBytes>>20)
	}
	if len(data) < 288 || data[0] != 0xE9 || binary.LittleEndian.Uint16(data[12:14]) != 9 ||
		binary.LittleEndian.Uint32(data[32:36]) != 0xABCD5432 {
		return fmt.Errorf("请选择 ESP32-S3 的 OTA 应用固件；-full 合并镜像仅用于 USB 刷机")
	}
	version = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")
	if !release.ParseVersion(version).OK {
		return fmt.Errorf("请输入有效的固件版本，例如 0.2.0")
	}
	s.firmwareMu.Lock()
	defer s.firmwareMu.Unlock()
	ctx := context.Background()
	active, err := s.st.Setting(ctx, "firmware_slot")
	if err != nil {
		return err
	}
	slot := "a"
	if active == "a" {
		slot = "b"
	}
	path = s.firmwareSlotPath(slot)
	// The directory is created by Boot, but a test server or a hand-rolled
	// Config may not have it yet.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	// A crash while replacing a staged file must not leave its old version
	// pointing at new bytes. The active file and version remain untouched.
	if err := s.st.PutSettings(ctx, map[string]string{
		"firmware_pending_version": "", "firmware_pending_slot": "",
	}); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return s.st.PutSettings(ctx, map[string]string{
		"firmware_pending_version": version,
		"firmware_pending_slot":    slot,
	})
}

// ApplyPendingFirmware publishes the staged image to the boards. This is the
// "推送给设备" button: until it runs, the image only occupies the OTA slot and
// every board keeps its current version.
func (s *Server) ApplyPendingFirmware(ctx context.Context) (string, error) {
	s.firmwareMu.Lock()
	defer s.firmwareMu.Unlock()
	settings, err := s.st.Settings(ctx)
	if err != nil {
		return "", err
	}
	pending := strings.TrimSpace(settings["firmware_pending_version"])
	if pending == "" {
		return "", fmt.Errorf("没有等待推送的固件")
	}
	path := s.firmwareSlotPath(settings["firmware_pending_slot"])
	if path == "" {
		return "", fmt.Errorf("上传目录未配置")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("固件文件不见了：%w", err)
	}
	if err := s.st.PutSettings(ctx, map[string]string{
		"firmware_version":         pending,
		"firmware_slot":            settings["firmware_pending_slot"],
		"firmware_pending_version": "",
		"firmware_pending_slot":    "",
	}); err != nil {
		return "", err
	}
	s.ForceFullRefresh()
	return pending, nil
}
