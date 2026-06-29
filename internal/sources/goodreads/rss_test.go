package goodreads

import "testing"

const sampleRSS = `<?xml version="1.0"?><rss><channel>
<item>
 <book_id>12345</book_id>
 <title>El Nombre del Viento &amp; Other Tales</title>
 <author_name>Patrick Rothfuss</author_name>
 <isbn>8401352835</isbn>
</item>
<item>
 <book_id>67890</book_id>
 <title>The Wise Man&apos;s Fear</title>
 <author_name>Patrick Rothfuss</author_name>
 <isbn></isbn>
</item>
</channel></rss>`

func TestParseRSS(t *testing.T) {
	books, err := parseRSS([]byte(sampleRSS), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("want 2 books, got %d", len(books))
	}
	if books[0].ExternalID != "12345" || books[0].Source != "goodreads" {
		t.Fatalf("bad identity: %+v", books[0])
	}
	if books[0].Title != "El Nombre del Viento & Other Tales" {
		t.Fatalf("entities not decoded: %q", books[0].Title)
	}
	if books[1].ISBN10 != "" {
		t.Fatalf("empty isbn should stay empty, got %q", books[1].ISBN10)
	}
	if books[0].Shelves[0] != "to-read" {
		t.Fatalf("shelf not tagged: %v", books[0].Shelves)
	}
}
