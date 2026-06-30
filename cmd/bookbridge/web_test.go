package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunWebUnknownStillDispatches(t *testing.T) {
	// `web` must be a recognized subcommand (not the usage code 2). Point BB_DB at
	// an unopenable path (its parent is a regular file) so runWeb fails fast at
	// store.Open and returns 1 — never reaching the blocking ListenAndServe.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"BB_DB": filepath.Join(f, "bb.db")}
	var out stringSink
	code := run([]string{"web"}, func(k string) string { return env[k] }, &out)
	if code == 2 {
		t.Fatalf("`web` should be dispatched, not usage error; out=%s", out.String())
	}
}

type stringSink struct{ b []byte }

func (s *stringSink) Write(p []byte) (int, error) { s.b = append(s.b, p...); return len(p), nil }
func (s *stringSink) String() string              { return string(s.b) }
