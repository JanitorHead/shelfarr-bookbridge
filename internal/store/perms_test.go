package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDBFileIsOwnerOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bb.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if mode := fi.Mode().Perm(); mode&0o077 != 0 {
			t.Fatalf("db file mode = %o, want no group/other bits", mode)
		}
	}
}
