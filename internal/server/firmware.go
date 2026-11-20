package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxFirmwareBytes = 8 << 20

func (s *Server) firmwarePath() string {
	if s.cfg.Paths.Firmware != "" {
		return filepath.Join(s.cfg.Paths.Firmware, "firmware.bin")
	}
	if s.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "firmware.bin")
}

// handleFirmware serves the uploaded ESP32 image to an authenticated device.
// The ETag lets a device skip a download it already has.
func (s *Server) handleFirmware(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.authorizeDevice(r, id); !ok {
		http.NotFound(w, r)
		return
	}
	path := s.firmwarePath()
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
	version, _ := s.st.Setting(r.Context(), "firmware_version")

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

// saveFirmware stores a multipart upload atomically and records its version.
// Only an ESP32 image (magic 0xE9) is accepted; Update on the device performs
// the real checksum and signature validation before writing flash.
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

// StageFirmware writes an ESP32 image to the OTA slot and records its version.
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
	if data[0] != 0xE9 {
		return fmt.Errorf("这不是 ESP32 固件（缺少 0xE9 头）")
	}
	if version == "" {
		version = time.Now().Format("20060102-150405")
	}
	// The directory is created by Boot, but a test server or a hand-rolled
	// Config may not have it yet.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Staged, not live: the image sits in the OTA slot but the version the
	// boards compare against is only published once a person says so. Otherwise
	// every board upgrades on its next poll without anyone asking.
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version = strings.TrimPrefix(version, "V")
	return s.st.PutSettings(context.Background(), map[string]string{
		"firmware_pending_version": version,
	})
}

// ApplyPendingFirmware publishes the staged image to the boards. This is the
// "推送给设备" button: until it runs, the image only occupies the OTA slot and
// every board keeps its current version.
func (s *Server) ApplyPendingFirmware(ctx context.Context) (string, error) {
	pending, err := s.st.Setting(ctx, "firmware_pending_version")
	if err != nil {
		return "", err
	}
	pending = strings.TrimSpace(pending)
	if pending == "" {
		return "", fmt.Errorf("没有等待推送的固件")
	}
	if _, err := s.firmwarePathStat(); err != nil {
		return "", fmt.Errorf("固件文件不见了：%w", err)
	}
	if err := s.st.PutSettings(ctx, map[string]string{
		"firmware_version":         pending,
		"firmware_pending_version": "",
	}); err != nil {
		return "", err
	}
	s.ForceFullRefresh()
	return pending, nil
}

// firmwarePathStat proves the OTA slot really holds a file.
func (s *Server) firmwarePathStat() (string, error) {
	path := s.firmwarePath()
	if path == "" {
		return "", fmt.Errorf("上传目录未配置")
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
