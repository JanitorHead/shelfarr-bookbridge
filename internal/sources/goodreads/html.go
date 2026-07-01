package goodreads

import (
	"bytes"
	"html"
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

// grHTMLDateLayouts are the display formats Goodreads renders date columns in,
// distinct from the RFC-style dates in RSS (grDateLayouts). date_read/started
// use "Jan 02, 2006" (span.<field>_value); date_added's span[title] is the long
// "January 02, 2006".
var grHTMLDateLayouts = []string{"Jan 02, 2006", "January 02, 2006", "Jan 2006", "2006/01/02", "Jan 02 2006"}

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

// dateCell reads a review/list date field. date_read/date_started put the shown
// date in span.<field>_value (e.g. span.date_read_value → "Apr 11, 2017");
// date_added is a span[title="January 02, 2006"] with the short date as text.
func dateCell(row *goquery.Selection, field string) time.Time {
	cell := row.Find("td.field." + field + " div.value")
	// .First(): a re-read book has several <span.date_read_value> — take the latest
	// shown (Goodreads lists the most recent first).
	if v := strings.TrimSpace(cell.Find("span." + field + "_value").First().Text()); v != "" {
		return grDate(v)
	}
	if s := cell.Find("span[title]").First(); s.Length() > 0 {
		if t := grDate(strings.TrimSpace(s.AttrOr("title", ""))); !t.IsZero() {
			return t
		}
		if t := grDate(strings.TrimSpace(s.Text())); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// htmlUserRating reads the user's own rating (0–5). Current Goodreads renders it
// as div.stars[data-rating]; older/static rows use filled "p10" star spans.
func htmlUserRating(row *goquery.Selection) int {
	if v, ok := row.Find("td.field.rating div.stars").Attr("data-rating"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			if n < 0 {
				n = 0
			}
			if n > 5 {
				n = 5
			}
			return n
		}
	}
	n := row.Find("td.field.rating .p10").Length()
	if n > 5 {
		n = 5
	}
	return n
}

var brRe = regexp.MustCompile(`(?i)<br\s*/?>`)
var tagRe = regexp.MustCompile(`<[^>]+>`)

// reviewText reads the user's full review from a review cell. Goodreads puts the
// full text in a hidden span#freeTextreview<id> (the visible span#freeTextContainer…
// is truncated); <br> become newlines. Empty for "Write a review".
func reviewText(row *goquery.Selection) string {
	cell := row.Find("td.field.review div.value")
	sel := cell.Find(`span[id^="freeTextreview"]`)
	if sel.Length() == 0 {
		sel = cell.Find(`span[id^="freeTextContainerreview"]`)
	}
	if sel.Length() == 0 {
		return ""
	}
	h, _ := sel.First().Html()
	h = brRe.ReplaceAllString(h, "\n")
	txt := html.UnescapeString(tagRe.ReplaceAllString(h, ""))
	return strings.TrimSpace(txt)
}

// notesText reads the user's private notes from a notes cell ("None" → empty).
func notesText(row *goquery.Selection) string {
	cell := row.Find("td.field.notes div.value").Clone()
	cell.Find("a").Remove() // drop the "[edit]" link
	t := strings.TrimSpace(cell.Text())
	if t == "" || strings.EqualFold(t, "None") {
		return ""
	}
	return t
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
			Review:        reviewText(row),
			Notes:         notesText(row),
		})
	})
	return out, false, nil
}
