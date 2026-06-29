package goodreads

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

type RSSSource struct {
	userID  string
	feedKey config.SecretString
	base    string // default https://www.goodreads.com
	hc      *http.Client
}

func NewRSSSource(userID string, feedKey config.SecretString, base string, hc *http.Client) *RSSSource {
	if base == "" {
		base = "https://www.goodreads.com"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &RSSSource{userID: userID, feedKey: feedKey, base: base, hc: hc}
}

func (s *RSSSource) Fetch(ctx context.Context, shelves []string) ([]sources.Book, error) {
	var all []sources.Book
	for _, shelf := range shelves {
		q := url.Values{"shelf": {shelf}}
		if k := s.feedKey.Reveal(); k != "" {
			q.Set("key", k)
		}
		u := fmt.Sprintf("%s/review/list_rss/%s?%s", s.base, url.PathEscape(s.userID), q.Encode())
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "shelfarr-bookbridge/0.1 (+self-hosted)")
		resp, err := s.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("goodreads fetch %q: %w", shelf, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("goodreads fetch %q: HTTP %d", shelf, resp.StatusCode)
		}
		books, err := parseRSS(body, shelf)
		if err != nil {
			return nil, fmt.Errorf("goodreads parse %q: %w", shelf, err)
		}
		if len(books) == 100 {
			log.Printf("WARNING: shelf %q returned exactly 100 items — RSS caps at 100; books beyond 100 need the cookie-HTML reader (Plan 2)", shelf)
		}
		all = append(all, books...)
	}
	return all, nil
}
