package goodreads

import (
	"sort"
	"testing"
)

// Invariant: the RSS and HTML parsers must emit identical ExternalID sets for
// the same shelf, or crossing the 100-cap re-requests every book.
func TestRSSAndHTMLExternalIDsMatch(t *testing.T) {
	rss, err := parseRSS([]byte(sampleRSS), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	html, _, err := parseHTMLList([]byte(sampleHTML), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	ids := func(bs []idHaver) []string {
		var out []string
		for _, b := range bs {
			out = append(out, b.id())
		}
		sort.Strings(out)
		return out
	}
	_ = ids
	var rssIDs, htmlIDs []string
	for _, b := range rss {
		rssIDs = append(rssIDs, b.ExternalID)
	}
	for _, b := range html {
		htmlIDs = append(htmlIDs, b.ExternalID)
	}
	sort.Strings(rssIDs)
	sort.Strings(htmlIDs)
	if len(rssIDs) != len(htmlIDs) {
		t.Fatalf("id count differs: rss=%v html=%v", rssIDs, htmlIDs)
	}
	for i := range rssIDs {
		if rssIDs[i] != htmlIDs[i] {
			t.Fatalf("id mismatch at %d: rss=%q html=%q", i, rssIDs[i], htmlIDs[i])
		}
	}
}

type idHaver interface{ id() string }

func TestNewSourceSelectsByCookie(t *testing.T) {
	withCookie := NewSource("42", "", "sess=abc", "", nil)
	if _, ok := withCookie.(*HTMLSource); !ok {
		t.Fatalf("cookie set should give *HTMLSource, got %T", withCookie)
	}
	noCookie := NewSource("42", "feedkey", "", "", nil)
	if _, ok := noCookie.(*RSSSource); !ok {
		t.Fatalf("no cookie should give *RSSSource, got %T", noCookie)
	}
}
