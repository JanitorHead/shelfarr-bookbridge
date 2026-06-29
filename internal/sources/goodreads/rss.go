package goodreads

import (
	"encoding/xml"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

type rssItem struct {
	BookID string `xml:"book_id"`
	Title  string `xml:"title"`
	Author string `xml:"author_name"`
	ISBN   string `xml:"isbn"`
}

type rssDoc struct {
	Items []rssItem `xml:"channel>item"`
}

// parseRSS decodes a Goodreads shelf RSS feed. encoding/xml resolves standard
// entities and does not expand external entities (safe by default).
func parseRSS(data []byte, shelf string) ([]sources.Book, error) {
	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make([]sources.Book, 0, len(doc.Items))
	for _, it := range doc.Items {
		id := strings.TrimSpace(it.BookID)
		if id == "" {
			continue // no identity — skip
		}
		out = append(out, sources.Book{
			Source:     "goodreads",
			ExternalID: id,
			Title:      strings.TrimSpace(it.Title),
			Author:     strings.TrimSpace(it.Author),
			ISBN10:     strings.TrimSpace(it.ISBN),
			Shelves:    []string{shelf},
		})
	}
	return out, nil
}
