package goodreads

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/PuerkitoBio/goquery"
)

var bookIDRe = regexp.MustCompile(`/book/show/(\d+)`)

// coverSrc reads a real cover URL from a Goodreads <img>, tolerating lazy-loaded
// images (data-src / data-original) and ignoring the "nophoto" placeholder.
func coverSrc(img *goquery.Selection) string {
	for _, attr := range []string{"src", "data-src", "data-original"} {
		v, ok := img.Attr(attr)
		v = strings.TrimSpace(v)
		if ok && v != "" && !strings.Contains(v, "nophoto") {
			return v
		}
	}
	return ""
}

// grHTMLDateLayouts are the display formats Goodreads renders date columns in
// (print view), distinct from the RFC-style dates in RSS (grDateLayouts).
var grHTMLDateLayouts = []string{"Jan 02, 2006", "Jan 2006", "2006/01/02", "Jan 02 2006"}

// grDate parses a Goodreads date cell, returning the zero time for blanks or
// the "not set" placeholder. It tolerates the few formats Goodreads uses.
func grDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "not set") {
		return time.Time{}
	}
	for _, layout := range grHTMLDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// dateCell reads a review/list date field's display value (Goodreads wraps the
// shown date in span.date_<field>_value inside the cell, falling back to the
// cell text).
func dateCell(row *goquery.Selection, field string) time.Time {
	cell := row.Find("td.field." + field + " div.value")
	if v := strings.TrimSpace(cell.Find("span.date_" + field + "_value").Text()); v != "" {
		return grDate(v)
	}
	return grDate(cell.Text())
}

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
		cover := coverSrc(row.Find("td.field.cover img"))
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
			AddedAt:       dateCell(row, "date_added"),
			StartedAt:     dateCell(row, "date_started"),
			ReadAt:        dateCell(row, "date_read"),
		})
	})
	return out, false, nil
}
