package app

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// teeLog sends the log to stderr and to logs/today.log. The file is opened for
// append and never rotated here: it is a diagnostic aid, and a runaway log is
// better than a missing one. A failure to open it is not fatal; the caller
// keeps stderr alone.
func teeLog(logsDir string) (*log.Logger, func(), error) {
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(logsDir, "today.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	w := io.MultiWriter(os.Stderr, f)
	return log.New(w, "", log.LstdFlags), func() { _ = f.Close() }, nil
}
