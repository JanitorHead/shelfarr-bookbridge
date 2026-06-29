package goodreads

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/PuerkitoBio/goquery"
)

var bookIDRe = regexp.MustCompile(`/book/show/(\d+)`)

// parseHTMLList parses a Goodreads review/list table page. signedOut is true
// when the page is the login wall (so the caller can surface ErrCookieExpired).
func parseHTMLList(data []byte, shelf string) ([]sources.Book, bool, error) {
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
	var out []sources.Book
	doc.Find("tbody#booksBody > tr").Each(func(_ int, row *goquery.Selection) {
		href, _ := row.Find("td.field.title div.value a").Attr("href")
		m := bookIDRe.FindStringSubmatch(href)
		if m == nil {
			return // malformed row — skip
		}
		title := strings.TrimSpace(row.Find("td.field.title div.value a").Text())
		author := strings.TrimSpace(row.Find("td.field.author div.value a").Text())
		isbn := strings.TrimSpace(row.Find("td.field.isbn div.value").Text())
		out = append(out, sources.Book{
			Source:     "goodreads",
			ExternalID: m[1],
			Title:      title,
			Author:     author,
			ISBN10:     isbn,
			Shelves:    []string{shelf},
		})
	})
	return out, false, nil
}
