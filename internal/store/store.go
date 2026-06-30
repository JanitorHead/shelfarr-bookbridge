package store

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

const schemaVersion = 6

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	// No WAL: WAL needs a shared-memory mmap on the DB's directory, which FUSE
	// filesystems (Unraid's /mnt/user shfs) don't support -> "unable to open
	// database file (14)". The default rollback journal works everywhere, and we
	// are single-writer (MaxOpenConns=1), so WAL buys nothing here.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single serialized writer
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot open the database at %q: %w; "+
			"on Unraid, map /config to a real disk path (e.g. /mnt/cache/appdata/shelfarr-bookbridge), "+
			"not the /mnt/user FUSE share", path, err)
	}
	s := &Store{db: db}
	_ = os.Chmod(path, 0o600) // best-effort; tighten secrets at rest on Linux
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying *sql.DB for tests and advanced callers.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return err
	}
	if ver > schemaVersion {
		return fmt.Errorf("db schema v%d is newer than supported v%d; upgrade BookBridge", ver, schemaVersion)
	}
	migrations := []string{
		// v1
		`CREATE TABLE IF NOT EXISTS books (
  source TEXT NOT NULL, external_id TEXT NOT NULL, title TEXT NOT NULL, author TEXT NOT NULL,
  isbn10 TEXT, year INTEGER, cover_url TEXT, added_at TEXT,
  first_seen_at TEXT NOT NULL DEFAULT (datetime('now')), state TEXT NOT NULL,
  work_id TEXT, chosen_language TEXT, chosen_format TEXT, shelfarr_request_id TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (source, external_id));
CREATE INDEX IF NOT EXISTS idx_books_state ON books(state);
CREATE TABLE IF NOT EXISTS shelf_config (
  shelf TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 1, baselined_at TEXT, format TEXT, language TEXT);`,
		// v2
		`CREATE TABLE IF NOT EXISTS book_shelves (
  source TEXT NOT NULL, external_id TEXT NOT NULL, shelf TEXT NOT NULL,
  PRIMARY KEY (source, external_id, shelf));
CREATE INDEX IF NOT EXISTS idx_book_shelves_shelf ON book_shelves(shelf);`,
		// v3
		`CREATE TABLE IF NOT EXISTS run_state (
  id INTEGER PRIMARY KEY CHECK (id = 1), running INTEGER NOT NULL DEFAULT 0, started_at TEXT);
INSERT OR IGNORE INTO run_state(id, running) VALUES (1, 0);`,
		// v4
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
		// v5
		`CREATE TABLE IF NOT EXISTS runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT NOT NULL, finished_at TEXT NOT NULL,
  mode TEXT NOT NULL, ok INTEGER NOT NULL,
  fetched INTEGER NOT NULL DEFAULT 0, new INTEGER NOT NULL DEFAULT 0,
  requested INTEGER NOT NULL DEFAULT 0, not_found INTEGER NOT NULL DEFAULT 0,
  errors INTEGER NOT NULL DEFAULT 0, summary TEXT NOT NULL DEFAULT '', error_text TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_runs_started ON runs(started_at DESC);`,
		// v6: live-progress fields on the single run_state row
		`ALTER TABLE run_state ADD COLUMN total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE run_state ADD COLUMN done INTEGER NOT NULL DEFAULT 0;
ALTER TABLE run_state ADD COLUMN current TEXT NOT NULL DEFAULT '';
ALTER TABLE run_state ADD COLUMN p_requested INTEGER NOT NULL DEFAULT 0;
ALTER TABLE run_state ADD COLUMN p_not_found INTEGER NOT NULL DEFAULT 0;
ALTER TABLE run_state ADD COLUMN p_failed INTEGER NOT NULL DEFAULT 0;`,
	}
	for i := ver; i < schemaVersion; i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration to v%d: %w", i+1, err)
		}
	}
	if ver < schemaVersion {
		if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
	}
	return nil
}
