package goodreads

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/PuerkitoBio/goquery"
)

// Goodreads Kindle "Notes & Highlights": /notes/<userID>/load_more returns a JSON
// list of the user's annotated books (paginated by next_token); each book's
// highlights are server-rendered on /notes/<bookID>/<userID>.
var notesBookIDRe = regexp.MustCompile(`/notes/(\d+)`)

type annotatedBooksResp struct {
	Collection []struct {
		ReadingNotesURL string `json:"readingNotesUrl"`
	} `json:"annotated_books_collection"`
	NextToken *string `json:"next_token"`
}

// FetchHighlights returns the user's Kindle highlights keyed by Goodreads book id
// (== our external_id). Best-effort and throttled; only books with annotations are
// visited, so it's a handful of requests, not one per library book.
func (s *HTMLSource) FetchHighlights(ctx context.Context) (map[string][]sources.Highlight, error) {
	bookIDs, err := s.annotatedBookIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]sources.Highlight{}
	for _, bid := range bookIDs {
		hs, err := s.bookHighlights(ctx, bid)
		if err != nil {
			continue // skip a book that fails, don't abort the whole pass
		}
		if len(hs) > 0 {
			out[bid] = hs
		}
		time.Sleep(jitterDelay()) // politeness between books
	}
	return out, nil
}

// annotatedBookIDs walks /notes/<userID>/load_more (following next_token) and
// returns the Goodreads book ids of every annotated book.
func (s *HTMLSource) annotatedBookIDs(ctx context.Context) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	token := ""
	for page := 0; page < 30; page++ { // safety bound
		u := fmt.Sprintf("%s/notes/%s/load_more", s.base, s.userID)
		if token != "" {
			u += "?next_token=" + token
		}
		body, err := s.getWithHeaders(ctx, u, true)
		if err != nil {
			return ids, err
		}
		var r annotatedBooksResp
		if err := json.Unmarshal(body, &r); err != nil {
			return ids, err
		}
		for _, b := range r.Collection {
			if m := notesBookIDRe.FindStringSubmatch(b.ReadingNotesURL); m != nil && !seen[m[1]] {
				seen[m[1]] = true
				ids = append(ids, m[1])
			}
		}
		if r.NextToken == nil || *r.NextToken == "" || len(r.Collection) == 0 {
			break
		}
		token = *r.NextToken
	}
	return ids, nil
}

// bookHighlights parses the server-rendered highlights on a book's notes page.
func (s *HTMLSource) bookHighlights(ctx context.Context, bookID string) ([]sources.Highlight, error) {
	u := fmt.Sprintf("%s/notes/%s/%s", s.base, bookID, s.userID)
	body, err := s.getWithHeaders(ctx, u, false)
	if err != nil {
		return nil, err
	}
	return parseHighlights(body)
}

// parseHighlights extracts the highlights server-rendered on a Goodreads notes
// page. Each is a div[data-annotation-pair-id] with the passage in
// .noteHighlightTextContainer__highlightText, an optional note, and a location.
func parseHighlights(body []byte) ([]sources.Highlight, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var out []sources.Highlight
	doc.Find("div[data-annotation-pair-id]").Each(func(_ int, row *goquery.Selection) {
		text := strings.TrimSpace(row.Find("div.noteHighlightTextContainer__highlightText span").First().Text())
		if text == "" {
			return
		}
		note := ""
		if nc := row.Find("div.noteHighlightTextContainer__noteContainer span").First(); nc.Length() > 0 {
			note = strings.TrimSpace(nc.Text())
		}
		out = append(out, sources.Highlight{
			Location: strings.TrimSpace(row.Find("div.noteHighlightContainer__location").First().Text()),
			Text:     text,
			Note:     note,
		})
	})
	return out, nil
}

// getWithHeaders GETs a URL with the browser UA + cookie; xhr adds the XHR header
// used by the load_more JSON endpoint.
func (s *HTMLSource) getWithHeaders(ctx context.Context, url string, xhr bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Cookie", strings.TrimSpace(s.cookie.Reveal()))
	if xhr {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, ErrCookieExpired
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("goodreads notes: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
