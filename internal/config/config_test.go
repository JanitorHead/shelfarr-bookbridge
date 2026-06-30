package config

import "testing"

func TestLoadDefaultsAndParsing(t *testing.T) {
	env := map[string]string{
		"SHELFARR_URL":   "https://s.example",
		"SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42",
		"SHELVES":        "to-read, sci-fi",
	}
	c, err := loadFrom(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.ShelfarrURL != "https://s.example" || c.ShelfarrToken.Reveal() != "shf_t" {
		t.Fatalf("bad shelfarr cfg: %+v", c)
	}
	if len(c.Shelves) != 2 || c.Shelves[1] != "sci-fi" {
		t.Fatalf("shelves not trimmed/split: %#v", c.Shelves)
	}
	if c.SimilarityThreshold != 0.82 || c.Format != "ebook" || c.FirstRun != "baseline" || c.MaxRequestsPerRun != 25 {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestGoodreadsModeLoaded(t *testing.T) {
	env := map[string]string{"GOODREADS_MODE": "private_cookie"}
	c, err := loadFrom(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.GoodreadsMode != "private_cookie" {
		t.Fatalf("GoodreadsMode = %q, want private_cookie", c.GoodreadsMode)
	}
}

func TestMissingShelfarrIsNotALoadError(t *testing.T) {
	// The daemon/GUI must start without Shelfarr config so it can be set up in
	// the GUI; missing URL/token is reported by ShelfarrConfigured(), not Load.
	c, err := loadFrom(func(string) string { return "" })
	if err != nil {
		t.Fatalf("missing Shelfarr config must not error at load: %v", err)
	}
	if c.ShelfarrConfigured() {
		t.Fatal("ShelfarrConfigured() should be false when URL/token are empty")
	}
}
