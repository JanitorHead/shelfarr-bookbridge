package hardcover

import (
	"context"
	"testing"
)

const sampleResp = `{"data":{"me":[{"user_books":[
{"book":{"id":101,"title":"Dune","release_year":1965,"contributions":[{"author":{"name":"Frank Herbert"}}]}},
{"book":{"id":102,"title":"El nombre del viento","release_year":2007,"contributions":[{"author":{"name":"Patrick Rothfuss"}}]}}
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
	if books[1].Year != 2007 || books[1].Shelves[0] != "want-to-read" {
		t.Fatalf("book1: %+v", books[1])
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
