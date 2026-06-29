package resolver

import (
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestTitleSimilaritySubtitleTolerant(t *testing.T) {
	// Metadata source dropped the ": A Primer" subtitle — still a strong match.
	if s := TitleSimilarity("Thinking In Systems: A Primer", "Thinking in systems"); s < 0.95 {
		t.Fatalf("subtitle-tolerant match too low: %f", s)
	}
	// But distinct works must NOT become strong matches.
	if s := TitleSimilarity("Dune", "Dune Messiah"); s >= 0.82 {
		t.Fatalf("Dune vs Dune Messiah must stay below threshold: %f", s)
	}
	if s := TitleSimilarity("El mono desnudo", "The Naked Ape"); s > 0.4 {
		t.Fatalf("different-language different-title should be low: %f", s)
	}
}

func TestResolveMatchesAcrossSubtitleAndAuthorOrder(t *testing.T) {
	// Real case from live testing: Goodreads "Last, First" + subtitle vs the
	// metadata source's "First Last" + no subtitle.
	b := sources.Book{Title: "Thinking In Systems: A Primer", Author: "Meadows, Donella H."}
	res := []shelfarr.SearchResult{
		{WorkID: "openlibrary:OL3737036W", Title: "Thinking in systems", Author: "Donella H. Meadows"},
	}
	pick, reason := Resolve(b, res, 0.82)
	if pick == nil {
		t.Fatalf("should match across subtitle + author order, reason=%s", reason)
	}
	if pick.WorkID != "openlibrary:OL3737036W" {
		t.Fatalf("wrong pick: %+v", pick)
	}
}
