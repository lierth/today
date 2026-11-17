package server

import (
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
func (s *Server) saveFirmware(r *http.Request) (string, error) {
	path := s.firmwarePath()
	if path == "" {
		return "", fmt.Errorf("上传目录未配置")
	}
	file, _, err := r.FormFile("firmware")
	if err != nil {
		return "", fmt.Errorf("缺少固件文件")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFirmwareBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("固件文件为空")
	}
	if len(data) > maxFirmwareBytes {
		return "", fmt.Errorf("固件超过 %d MB", maxFirmwareBytes>>20)
	}
	if data[0] != 0xE9 {
		return "", fmt.Errorf("这不是 ESP32 固件（缺少 0xE9 头）")
	}
	version := strings.TrimSpace(r.FormValue("firmware_version"))
	if version == "" {
		version = time.Now().Format("20060102-150405")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := s.st.PutSettings(r.Context(), map[string]string{"firmware_version": version}); err != nil {
		return "", err
	}
	return version, nil
}
