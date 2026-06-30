package goodreads

import (
	"strings"
	"testing"
)

const richRSS = `<?xml version="1.0" encoding="UTF-8"?><rss><channel>
<item>
<book_id>101</book_id>
<title>Dune</title>
<author_name>Frank Herbert</author_name>
<isbn>0441013597</isbn>
<book_large_image_url><![CDATA[https://i.gr-assets.com/images/S/books/1/101._SY475_.jpg]]></book_large_image_url>
<book_description><![CDATA[Set on the desert planet Arrakis...]]></book_description>
<book_published>1965</book_published>
<user_rating>5</user_rating>
<average_rating>4.26</average_rating>
<user_date_added><![CDATA[Tue, 16 Aug 2016 06:37:30 -0700]]></user_date_added>
<user_read_at><![CDATA[Fri, 02 Sep 2016 00:00:00 -0700]]></user_read_at>
</item>
</channel></rss>`

func TestParseRSSRichMetadata(t *testing.T) {
	books, err := parseRSS([]byte(richRSS), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("want 1 book, got %d", len(books))
	}
	b := books[0]
	if b.Year != 1965 || b.UserRating != 5 || b.AverageRating != 4.26 {
		t.Fatalf("metadata: %+v", b)
	}
	if b.CoverURL == "" || strings.Contains(b.CoverURL, "_SY475_") {
		t.Fatalf("cover size suffix not stripped: %q", b.CoverURL)
	}
	if b.AddedAt.IsZero() || b.ReadAt.IsZero() {
		t.Fatalf("dates not parsed: added=%v read=%v", b.AddedAt, b.ReadAt)
	}
	if b.Description == "" {
		t.Fatalf("description empty")
	}
}

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
