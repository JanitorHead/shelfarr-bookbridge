package goodreads

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/PuerkitoBio/goquery"
)

var bookIDRe = regexp.MustCompile(`/book/show/(\d+)`)

// htmlUserRating reads the user's rating (0–5) from a review/list row; Goodreads
// renders it as filled-star spans (class "p10"). Best-effort: 0 when absent.
func htmlUserRating(row *goquery.Selection) int {
	n := row.Find("td.field.rating .p10").Length()
	if n > 5 {
		return 5
	}
	return n
}

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
		cover, _ := row.Find("td.field.cover img").Attr("src")
		avg, _ := strconv.ParseFloat(strings.TrimSpace(row.Find("td.field.avg_rating div.value").Text()), 64)
		out = append(out, sources.Book{
			Source:        "goodreads",
			ExternalID:    m[1],
			Title:         title,
			Author:        author,
			ISBN10:        isbn,
			Shelves:       []string{shelf},
			CoverURL:      upscaleGRCover(strings.TrimSpace(cover)),
			UserRating:    htmlUserRating(row),
			AverageRating: avg,
		})
	})
	return out, false, nil
}
