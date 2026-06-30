package hardcover

import (
	"context"
	"testing"
)

const sampleResp = `{"data":{"me":[{"user_books":[
{"rating":4.5,"status_id":2,"user_book_reads":[{"started_at":"2024-03-01","finished_at":null,"progress":42.0,"progress_pages":210,"edition":{"pages":500}}],"book":{"id":101,"title":"Dune","release_year":1965,"pages":412,"description":"Desert planet.","image":{"url":"https://hc/dune.jpg"},"contributions":[{"author":{"name":"Frank Herbert"}}]}},
{"rating":0,"status_id":1,"user_book_reads":[],"book":{"id":102,"title":"El nombre del viento","release_year":2007,"contributions":[{"author":{"name":"Patrick Rothfuss"}}]}}
]}]}}`

func TestParseUserBooks(t *testing.T) {
	books, err := parseUserBooks([]byte(sampleResp), "want-to-read")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("want 2 books, got %d", len(books))
	}
	if books[0].Source != "hardcover" || books[0].ExternalID != "101" || books[0].Title != "Dune" || books[0].Author != "Frank Herbert" {
		t.Fatalf("book0: %+v", books[0])
	}
	// Rich personal data: rating rounds 4.5→5, progress 42%, "page 210 of 500",
	// started date, cover + description captured.
	if books[0].UserRating != 5 {
		t.Fatalf("rating: got %d want 5", books[0].UserRating)
	}
	if books[0].ProgressPct != 42 || books[0].ProgressLabel != "page 210 of 500" {
		t.Fatalf("progress: %d %q", books[0].ProgressPct, books[0].ProgressLabel)
	}
	if got := books[0].StartedAt.Format("2006-01-02"); got != "2024-03-01" {
		t.Fatalf("started_at: %q", got)
	}
	if books[0].CoverURL != "https://hc/dune.jpg" || books[0].Description != "Desert planet." {
		t.Fatalf("cover/desc: %+v", books[0])
	}
	if books[1].Year != 2007 || books[1].Shelves[0] != "want-to-read" {
		t.Fatalf("book1: %+v", books[1])
	}
	if books[1].UserRating != 0 || books[1].ProgressPct != 0 || !books[1].ReadAt.IsZero() {
		t.Fatalf("book1 should have no personal data: %+v", books[1])
	}
}

func TestParseUserBooksAPIError(t *testing.T) {
	_, err := parseUserBooks([]byte(`{"errors":[{"message":"field 'me' not found"}]}`), "read")
	if err == nil {
		t.Fatal("expected an API error")
	}
}

func TestListShelves(t *testing.T) {
	s := NewSource("tok", "", nil)
	sh, _ := s.ListShelves(context.Background())
	if len(sh) != 3 || sh[0].Slug != "want-to-read" {
		t.Fatalf("shelves: %+v", sh)
	}
}
