package goodreads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

var ErrCookieExpired = errors.New("goodreads session cookie expired or invalid — re-grab it from your browser DevTools")

const maxHTMLPages = 50 // safety bound (50*100 = 5000 books)

type HTMLSource struct {
	userID string
	cookie config.SecretString
	base   string
	hc     *http.Client
}

func NewHTMLSource(userID string, cookie config.SecretString, base string, hc *http.Client) *HTMLSource {
	if base == "" {
		base = "https://www.goodreads.com"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTMLSource{userID: userID, cookie: cookie, base: base, hc: hc}
}

func (s *HTMLSource) Fetch(ctx context.Context, shelves []string) ([]sources.Book, error) {
	var all []sources.Book
	for _, shelf := range shelves {
		seen := map[string]struct{}{}
		for page := 1; page <= maxHTMLPages; page++ {
			q := url.Values{
				"shelf":    {shelf},
				"per_page": {"100"},
				"page":     {strconv.Itoa(page)},
				"print":    {"true"},
			}
			u := fmt.Sprintf("%s/review/list/%s?%s", s.base, url.PathEscape(s.userID), q.Encode())
			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "shelfarr-bookbridge/0.1 (+self-hosted)")
			req.Header.Set("Cookie", s.cookie.Reveal())
			resp, err := s.hc.Do(req)
			if err != nil {
				return nil, fmt.Errorf("goodreads html fetch %q p%d: %w", shelf, page, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				return nil, fmt.Errorf("goodreads html fetch %q p%d: HTTP %d", shelf, page, resp.StatusCode)
			}
			books, signedOut, err := parseHTMLList(body, shelf)
			if err != nil {
				return nil, err
			}
			if signedOut {
				return nil, ErrCookieExpired
			}
			if len(books) == 0 {
				break // sentinel / past the last page
			}
			for _, b := range books {
				if _, dup := seen[b.ExternalID]; dup {
					continue
				}
				seen[b.ExternalID] = struct{}{}
				all = append(all, b)
			}
			time.Sleep(jitterDelay()) // politeness between pages
		}
	}
	return all, nil
}

func jitterDelay() time.Duration { return 1500 * time.Millisecond }
