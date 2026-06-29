package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSecretStringRedacts(t *testing.T) {
	s := SecretString("shf_supersecret")
	if got := s.String(); strings.Contains(got, "supersecret") {
		t.Fatalf("String() leaked secret: %q", got)
	}
	if got := fmt.Sprintf("%v %s", s, s); strings.Contains(got, "supersecret") {
		t.Fatalf("format verbs leaked secret: %q", got)
	}
	b, _ := json.Marshal(struct{ T SecretString }{s})
	if strings.Contains(string(b), "supersecret") {
		t.Fatalf("JSON leaked secret: %s", b)
	}
	if s.Reveal() != "shf_supersecret" {
		t.Fatalf("Reveal() wrong: %q", s.Reveal())
	}
}
