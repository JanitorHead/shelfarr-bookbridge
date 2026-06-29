package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single serialized writer
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return err
	}
	if ver > schemaVersion {
		return fmt.Errorf("db schema v%d is newer than supported v%d; upgrade BookBridge", ver, schemaVersion)
	}
	if ver == schemaVersion {
		return nil
	}
	const ddl = `
CREATE TABLE IF NOT EXISTS books (
  source TEXT NOT NULL,
  external_id TEXT NOT NULL,
  title TEXT NOT NULL,
  author TEXT NOT NULL,
  isbn10 TEXT,
  year INTEGER,
  cover_url TEXT,
  added_at TEXT,
  first_seen_at TEXT NOT NULL DEFAULT (datetime('now')),
  state TEXT NOT NULL,
  work_id TEXT,
  chosen_language TEXT,
  chosen_format TEXT,
  shelfarr_request_id TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (source, external_id)
);
CREATE INDEX IF NOT EXISTS idx_books_state ON books(state);
CREATE TABLE IF NOT EXISTS shelf_config (
  shelf TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 1,
  baselined_at TEXT,
  format TEXT,
  language TEXT
);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return err
	}
	_, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}
