package resolver

import (
	"fmt"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// SearchQuery builds a clean Shelfarr metadata query from a Goodreads book:
// the main title (subtitle dropped), plus the author flipped from Goodreads'
// "Last, First" to "First Last", with whitespace/newlines collapsed. A long
// raw subtitle or a "Last, First" author noticeably worsens search recall.
func SearchQuery(title, author string) string {
	a := strings.TrimSpace(author)
	if i := strings.Index(a, ", "); i > 0 {
		a = strings.TrimSpace(a[i+2:]) + " " + strings.TrimSpace(a[:i])
	}
	return strings.Join(strings.Fields(MainTitle(title)+" "+a), " ")
}

type Pick struct {
	WorkID   string
	Title    string
	Author   string
	Year     int
	CoverURL string
	Score    float64
}

// Resolve is pure: it selects the search result whose combined title+author
// similarity to the book is highest and >= threshold. Ties break on Shelfarr
// confidence (corroboration), then on result order. has_ebook is NOT a gate.
func Resolve(b sources.Book, results []shelfarr.SearchResult, threshold float64) (*Pick, string) {
	var best *Pick
	var bestConf int
	for _, r := range results {
		titleSim := TitleSimilarity(b.Title, r.Title)
		var score float64
		if strings.TrimSpace(r.Author) == "" || strings.TrimSpace(b.Author) == "" {
			// author unknown on one side -> judge on title alone (the metadata
			// source sometimes returns a matching work with no author).
			score = titleSim
		} else {
			score = 0.7*titleSim + 0.3*Similarity(b.Author, r.Author)
		}
		if score < threshold {
			continue
		}
		conf := 0
		if r.Confidence != nil {
			conf = *r.Confidence
		}
		if best == nil || score > best.Score || (score == best.Score && conf > bestConf) {
			best = &Pick{WorkID: r.WorkID, Title: r.Title, Author: r.Author, Year: r.Year, CoverURL: r.CoverURL, Score: score}
			bestConf = conf
		}
	}
	if best == nil {
		return nil, fmt.Sprintf("no result >= similarity %.2f for %q by %q", threshold, b.Title, b.Author)
	}
	return best, ""
}
