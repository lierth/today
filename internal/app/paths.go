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
// Config value wins; SkipCloud means none; a --db override also means none.
//
// The --db rule is not a convenience: a database somewhere else belongs to a
// test, a second profile or a support case, and copying its snapshots into the
// person's real iCloud Drive mixes one profile's data into another's (and made
// a smoke test write into the real account). Those runs keep their backups
// beside their own database.
func cloudDocsFor(cfg Config, dbOverride string) string {
	if cfg.CloudDocs != "" {
		return cfg.CloudDocs
	}
	if cfg.SkipCloud || dbOverride != "" {
		return ""
	}
	_, docs := backup.CloudDocsForHome()
	return docs
}
