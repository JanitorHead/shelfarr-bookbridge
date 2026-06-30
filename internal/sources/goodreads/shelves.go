package goodreads

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/PuerkitoBio/goquery"
)

var shelfHrefRe = regexp.MustCompile(`[?&]shelf=([^&"']+)`)
var shelfCountRe = regexp.MustCompile(`\((\d[\d,]*)\)\s*$`)

// parseShelves extracts the user's shelves from a review/list page sidebar.
// signedOut is true when the page is the login wall.
func parseShelves(data []byte) ([]sources.Shelf, bool, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	if title := strings.ToLower(strings.TrimSpace(doc.Find("head title").Text())); strings.Contains(title, "sign in") {
		return nil, true, nil
	}
	if doc.Find("#choices, #signedOutBanner, #userSignInForm").Length() > 0 {
		return nil, true, nil
	}
	seen := map[string]bool{}
	var out []sources.Shelf
	doc.Find("a[href*='shelf=']").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if !strings.Contains(href, "/review/list") { // sidebar links only, not per-book menus
			return
		}
		m := shelfHrefRe.FindStringSubmatch(href)
		if m == nil {
			return
		}
		slug, err := url.QueryUnescape(m[1])
		if err != nil {
			slug = m[1]
		}
		slug = strings.TrimSpace(slug)
		if slug == "" || strings.Contains(slug, "#") || seen[slug] { // skip the %23ALL%23 pseudo-shelf + dupes
			return
		}
		text := strings.Join(strings.Fields(a.Text()), " ")
		count := 0
		if c := shelfCountRe.FindStringSubmatch(text); c != nil {
			count, _ = strconv.Atoi(strings.ReplaceAll(c[1], ",", ""))
			text = strings.TrimSpace(shelfCountRe.ReplaceAllString(text, ""))
		}
		name := text
		if name == "" {
			name = slug
		}
		seen[slug] = true
		out = append(out, sources.Shelf{Slug: slug, Name: name, Count: count})
	})
	return out, false, nil
}

// ListShelves enumerates the user's Goodreads shelves (requires the cookie).
func (s *HTMLSource) ListShelves(ctx context.Context) ([]sources.Shelf, error) {
	q := url.Values{"print": {"true"}, "per_page": {"1"}}
	u := fmt.Sprintf("%s/review/list/%s?%s", s.base, url.PathEscape(s.userID), q.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Cookie", strings.TrimSpace(s.cookie.Reveal()))
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("goodreads list shelves: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, ErrCookieExpired
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("goodreads list shelves: HTTP %d", resp.StatusCode)
	}
	shelves, signedOut, err := parseShelves(body)
	if err != nil {
		return nil, err
	}
	if signedOut {
		return nil, ErrCookieExpired
	}
	return shelves, nil
}
