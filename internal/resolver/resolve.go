package resolver

import (
	"fmt"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

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
		score := 0.7*TitleSimilarity(b.Title, r.Title) + 0.3*Similarity(b.Author, r.Author)
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
