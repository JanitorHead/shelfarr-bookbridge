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

func TestSearchQuery(t *testing.T) {
	// subtitle dropped, "Last, First" flipped, newline/whitespace collapsed
	got := SearchQuery("An Incomplete Education: 3,684 Things You Didn't", "Jones, Judy")
	if got != "An Incomplete Education Judy Jones" {
		t.Fatalf("got %q", got)
	}
	got2 := SearchQuery("Asimov's Guide to the Bible: The Old\n   (2 Vol.)", "Asimov, Isaac")
	if got2 != "Asimov's Guide to the Bible Isaac Asimov" {
		t.Fatalf("got %q", got2)
	}
}

func TestResolveMatchesWhenResultAuthorEmpty(t *testing.T) {
	// Shelfarr returned the right work with an empty author — a perfect title
	// match must still clear the bar instead of being dragged down by 0.3*0.
	b := sources.Book{Title: "Locos por los clásicos", Author: "Garrido, Carlos"}
	res := []shelfarr.SearchResult{{WorkID: "ol:9", Title: "Locos por los clásicos", Author: ""}}
	if pick, reason := Resolve(b, res, 0.82); pick == nil {
		t.Fatalf("empty-author result should still match on title, reason=%s", reason)
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
