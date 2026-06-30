package main

import (
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

// TestStatusShelfName checks reading-list shelves map to canonical Calibre-Web
// Shelf names, while "read" and topic shelves are not treated as reading lists.
func TestStatusShelfName(t *testing.T) {
	cases := map[string]struct {
		name string
		ok   bool
	}{
		"to-read":           {"To Read", true},
		"want-to-read":      {"To Read", true},
		"currently-reading": {"Currently Reading", true},
		"dnf":               {"Did Not Finish", true},
		"did-not-finish":    {"Did Not Finish", true},
		"read":              {"", false}, // finished → read flag, not a shelf
		"ciencia":           {"", false}, // topic → a tag, not a shelf
	}
	for slug, want := range cases {
		got, ok := statusShelfName(slug)
		if got != want.name || ok != want.ok {
			t.Errorf("statusShelfName(%q) = (%q,%t), want (%q,%t)", slug, got, ok, want.name, want.ok)
		}
	}
}

// TestPushFieldRouting verifies the field-routing invariant: topic shelves go to
// tags, reading-list statuses go to Shelves, and "read" goes to neither (the read
// flag handles it) — mirroring how cwaTagPass splits a book's shelves.
func TestPushFieldRouting(t *testing.T) {
	shelves := []string{"to-read", "read", "ciencia", "politica", "currently-reading"}
	topics := store.TopicTags(shelves)
	if len(topics) != 2 || topics[0] != "ciencia" || topics[1] != "politica" {
		t.Fatalf("topics should be the non-status shelves, got %v", topics)
	}
	var lists []string
	for _, sh := range shelves {
		if name, ok := statusShelfName(sh); ok {
			lists = append(lists, name)
		}
	}
	// to-read + currently-reading become reading lists; read does NOT.
	if len(lists) != 2 {
		t.Fatalf("reading lists should be 2 (To Read, Currently Reading), got %v", lists)
	}
}
