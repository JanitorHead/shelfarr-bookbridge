package resolver

import (
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func ci(i int) *int { return &i }

func TestResolvePicksBestAboveThreshold(t *testing.T) {
	b := sources.Book{Title: "Dune", Author: "Frank Herbert"}
	res := []shelfarr.SearchResult{
		{WorkID: "a", Title: "Dune Messiah", Author: "Frank Herbert", Confidence: ci(70)},
		{WorkID: "b", Title: "Dune", Author: "Frank Herbert", Confidence: ci(70)},
	}
	pick, reason := Resolve(b, res, 0.82)
	if pick == nil {
		t.Fatalf("expected a pick, reason=%s", reason)
	}
	if pick.WorkID != "b" {
		t.Fatalf("expected exact-title work b, got %q", pick.WorkID)
	}
}

func TestResolveTiebreakOnConfidence(t *testing.T) {
	b := sources.Book{Title: "Dune", Author: "Frank Herbert"}
	res := []shelfarr.SearchResult{
		{WorkID: "low", Title: "Dune", Author: "Frank Herbert", Confidence: ci(70)},
		{WorkID: "high", Title: "Dune", Author: "Frank Herbert", Confidence: ci(100)},
	}
	pick, _ := Resolve(b, res, 0.82)
	if pick == nil || pick.WorkID != "high" {
		t.Fatalf("equal similarity should break on confidence -> high, got %+v", pick)
	}
}

func TestResolveNotFoundBelowThreshold(t *testing.T) {
	b := sources.Book{Title: "Dune", Author: "Frank Herbert"}
	res := []shelfarr.SearchResult{{WorkID: "x", Title: "War and Peace", Author: "Tolstoy"}}
	pick, reason := Resolve(b, res, 0.82)
	if pick != nil {
		t.Fatalf("expected not_found, got %+v", pick)
	}
	if reason == "" {
		t.Fatal("expected a reason string")
	}
}
