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

func TestLoadMissingRequired(t *testing.T) {
	if _, err := loadFrom(func(string) string { return "" }); err == nil {
		t.Fatal("expected error when SHELFARR_URL missing")
	}
}
