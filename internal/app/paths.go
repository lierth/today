package app

import (
	"log"
	"path/filepath"

	"today/internal/backup"
	"today/internal/datahome"
)

// PathsFor resolves the on-disk layout for one boot. It is the only place that
// knows about ~/.today, the legacy Application Support directory and --db.
//
// When the migration cannot complete, the returned Paths point at the legacy
// database so the app keeps running; `note` explains the situation for the log
// and the settings page.
func PathsFor(dbOverride string, logger *log.Logger) (datahome.Paths, string) {
	if dbOverride != "" {
		// An explicit --db is authoritative; nothing is migrated. The rest of
		// the layout still comes from the root so backups and config have a
		// home.
		return datahome.Resolve(datahome.Root(), dbOverride), ""
	}
	root := datahome.Root()
	res := datahome.Migrator{Root: root, Legacy: datahome.LegacyRoot(), Logf: logger.Printf}.Run()
	if res.Failed && res.UsedLegacyDB != "" {
		p := datahome.Resolve(filepath.Dir(res.UsedLegacyDB), res.UsedLegacyDB)
		return p, res.Reason
	}
	p := datahome.Resolve(root, "")
	if err := p.Ensure(); err != nil {
		logger.Printf("today: data directories: %v", err)
	}
	return p, res.Reason
}

// DefaultDBPath is the database path for a plain boot. Callers that need the
// whole layout (backups, config) use PathsFor.
func DefaultDBPath() string {
	return datahome.DBPath(datahome.Root())
}

// cloudDocsFor decides which iCloud folder this boot may use. An explicit
// Config value wins; SkipCloud means none; otherwise the real home is read
// here, once. Nothing else in the program looks iCloud up.
func cloudDocsFor(cfg Config) string {
	if cfg.CloudDocs != "" {
		return cfg.CloudDocs
	}
	if cfg.SkipCloud {
		return ""
	}
	_, docs := backup.CloudDocsForHome()
	return docs
}
