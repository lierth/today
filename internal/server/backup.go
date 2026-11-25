package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"today/internal/backup"
	"today/internal/store"
)

// ErrBackupBusy is returned when another backup or restore is running.
var ErrBackupBusy = errors.New("正在备份或恢复，请稍后再试")

// backupState serialises backups and restores: two VACUUM INTO runs into the
// same name would fight, and a restore must not race a backup.
type backupState struct {
	mu        sync.Mutex
	lastLocal string
	lastCloud string
	lastErr   string
	running   bool
	daily     func(day string) error
	lastDay   string
}

// BackupStatus is what the settings page shows.
type BackupStatus struct {
	LastLocal string // 2006-01-02 15:04
	LastCloud string
	Error     string
	CloudOn   bool
	CloudPath string
	Dir       string
	CloudDir  string
}

func (s *Server) backupDir() string { return s.cfg.Paths.Backups }

// cloudDir is the iCloud target, or "" when this process has no iCloud folder.
// The folder comes from Config.Paths (app.Boot resolves it once); there is no
// fallback to the real home here, so a test that forgets to set it writes
// nothing rather than into the person's iCloud.
func (s *Server) cloudDir() string {
	docs := s.cfg.Paths.CloudDocs
	if docs == "" {
		return ""
	}
	// Defence in depth: a test that somehow configured a path under the real
	// iCloud folder is a bug in the test, and it must fail loudly before it
	// writes anything.
	if testing.Testing() && looksLikeRealICloud(docs) {
		panic("server: test configured the real iCloud folder: " + docs)
	}
	if !backup.CloudAvailable(docs) {
		return ""
	}
	return backup.CloudDir(docs)
}

// looksLikeRealICloud reports whether docs lives under a real user's iCloud
// Drive rather than a temp directory.
func looksLikeRealICloud(docs string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	real := backup.CloudDocs(home)
	rel, err := filepath.Rel(real, docs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// RunBackup writes one local snapshot and, when the setting is on and iCloud
// exists, the cloud copy. Safe to call from a ticker.
func (s *Server) RunBackup(day string) error {
	if s.backupDir() == "" {
		return errors.New("没有配置数据目录")
	}
	s.backups.mu.Lock()
	if s.backups.running {
		s.backups.mu.Unlock()
		// Answering nil would tell the "立即备份" button it succeeded when
		// nothing was written, and would mark the day's automatic run done.
		return ErrBackupBusy
	}
	s.backups.running = true
	s.backups.mu.Unlock()
	defer func() {
		s.backups.mu.Lock()
		s.backups.running = false
		s.backups.mu.Unlock()
	}()

	now := s.clock()
	stamp := now.Format("2006-01-02 15:04")
	local, err := backup.Create(s.cfg.Paths.DB, s.backupDir(), "today", now)
	s.backups.mu.Lock()
	defer s.backups.mu.Unlock()
	if err != nil {
		s.backups.lastErr = err.Error()
		return err
	}
	s.backups.lastLocal = stamp
	s.backups.lastErr = ""

	cloud := s.cloudDir()
	if cloud == "" {
		return nil
	}
	settings, err := s.st.Settings(context.Background())
	if err != nil || settings["backup_icloud"] == "false" {
		return nil
	}
	if _, err := backup.CopyCloud(local, cloud, now); err != nil {
		s.backups.lastErr = "iCloud 备份失败：" + err.Error()
		return nil
	}
	s.backups.lastCloud = stamp
	return nil
}

// BackupStatus reports the last runs and where the files are.
func (s *Server) BackupStatus() BackupStatus {
	s.backups.mu.Lock()
	defer s.backups.mu.Unlock()
	out := BackupStatus{
		LastLocal: s.backups.lastLocal,
		LastCloud: s.backups.lastCloud,
		Error:     s.backups.lastErr,
		Dir:       s.backupDir(),
		CloudDir:  s.cloudDir(),
	}
	out.CloudOn = out.CloudDir != ""
	if docs := s.cfg.Paths.CloudDocs; docs != "" && out.CloudOn {
		out.CloudPath = backup.CloudDir(docs)
	}
	return out
}

// SnapshotList is every restorable file, local and cloud.
func (s *Server) SnapshotList() ([]backup.Snapshot, error) {
	local, err := backup.List(s.backupDir(), false)
	if err != nil {
		return nil, err
	}
	cloud, err := backup.List(s.cloudDir(), true)
	if err != nil && !os.IsNotExist(err) {
		// A missing cloud folder is normal; anything else is worth reporting
		// but must not hide the local backups.
		s.log.Printf("server: list cloud backups: %v", err)
	}
	out := make([]backup.Snapshot, 0, len(local)+len(cloud))
	out = append(out, local...)
	// Cloud copies with the same name shadow nothing: they are separate files
	// and the page says where each one lives.
	out = append(out, cloud...)
	return out, nil
}

// installBackupHook wires the once-a-day backup into the ticker. It is a no-op
// when the config has no backup directory (a bare server.New in a test).
func (s *Server) installBackupHook() {
	if s.cfg.Paths.Backups == "" {
		return
	}
	s.SetDailyHook(func(day string) error {
		for _, dir := range []string{s.backupDir(), s.cloudDir()} {
			if err := backup.PruneExpired(dir, s.clock()); err != nil {
				return err
			}
		}
		if err := s.RunBackup(day); err != nil {
			s.log.Printf("server: backup %s: %v", day, err)
			return err
		}
		return nil
	})
}

func (s *Server) DeleteSnapshot(name string) error {
	s.backups.mu.Lock()
	if s.backups.running {
		s.backups.mu.Unlock()
		return ErrBackupBusy
	}
	s.backups.running = true
	s.backups.mu.Unlock()
	defer func() { s.backups.mu.Lock(); s.backups.running = false; s.backups.mu.Unlock() }()
	snaps, err := s.SnapshotList()
	if err != nil {
		return err
	}
	snapshot, err := SnapshotByName(snaps, name)
	if err != nil {
		return err
	}
	return os.Remove(snapshot.Path)
}

// SetDailyBackupForTest installs the same hook a started server has; the test
// environment talks to the handler directly and never runs Start.
func (s *Server) SetDailyBackupForTest() error { s.installBackupHook(); return nil }

// SetDailyHook installs the callback the ticker runs once per local day.
func (s *Server) SetDailyHook(fn func(day string) error) {
	s.backups.mu.Lock()
	s.backups.daily = fn
	s.backups.mu.Unlock()
}

// runDailyHook runs the once-a-day callback when the local date has changed.
//
// The hook, not this function, decides what the day's work is (for the
// running server it is a backup); the ticker only owns the once-per-day gate.
// Marking the day after a successful run means a transient failure (a busy
// restore, a full disk) gets another chance on the next tick, while a
// successful run does not repeat.
func (s *Server) runDailyHook(now time.Time, loc *time.Location) {
	day := now.In(loc).Format(store.DateLayout)
	s.backups.mu.Lock()
	hooked := s.backups.daily
	if hooked == nil || day == s.backups.lastDay {
		s.backups.mu.Unlock()
		return
	}
	s.backups.mu.Unlock()
	if err := hooked(day); err == nil {
		s.backups.mu.Lock()
		s.backups.lastDay = day
		s.backups.mu.Unlock()
	}
}

// RestoreBackup copies a snapshot's rows back inside one transaction. The
// store stays open: SQLite serialises the writer, and a rollback leaves the
// current data exactly as it was.
func (s *Server) RestoreBackup(path string) error {
	dbPath := s.cfg.Paths.DB
	if dbPath == "" {
		return errors.New("没有配置数据库路径")
	}
	if err := backup.Restore(path, dbPath, s.backupDir(), s.clock()); err != nil {
		return err
	}
	// The snapshot has no session key (every backup blanks it), but the
	// running process still holds the old one in memory. Rotating now makes
	// every session signed by the pre-restore key invalid instead of leaving
	// them valid until the next restart.
	if err := s.RotateSessionKey(context.Background()); err != nil {
		s.log.Printf("server: restore rotated sessions: %v", err)
	}
	s.ForceFullRefresh()
	return nil
}
