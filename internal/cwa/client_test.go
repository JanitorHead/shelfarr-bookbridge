package cwa

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

// TestLoginTrailingNewlineNoPanic reproduces the daemon crash: a CWA URL with a
// trailing newline (common when pasted) must return an error, never panic Do
// with a nil request.
func TestLoginTrailingNewlineNoPanic(t *testing.T) {
	c := New("http://127.0.0.1:0\n ", "admin", config.SecretString("x"))
	// Must not panic; a connection error is fine.
	if err := c.Login(context.Background()); err == nil {
		t.Fatal("expected an error connecting to a dead address")
	}
}
