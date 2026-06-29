package store

import (
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bb.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != schemaVersion {
		t.Fatalf("user_version = %d, want %d", ver, schemaVersion)
	}
	// reopening must not error (idempotent migration)
	s.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	// books table must exist
	if _, err := s2.db.Exec(`INSERT INTO books(source,external_id,title,author,state) VALUES('goodreads','1','t','a','new')`); err != nil {
		t.Fatalf("insert into books: %v", err)
	}
}
