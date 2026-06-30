package goodreads

import (
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// grCoverSizeRe matches Goodreads' thumbnail size suffix (e.g. "._SY75_" or
// "._SX98_") so we can strip it for the full-size cover.
var grCoverSizeRe = regexp.MustCompile(`\._S[XY]\d+_`)

func upscaleGRCover(u string) string {
	if u == "" {
		return ""
	}
	return grCoverSizeRe.ReplaceAllString(u, "")
}

type rssItem struct {
	BookID        string `xml:"book_id"`
	Title         string `xml:"title"`
	Author        string `xml:"author_name"`
	ISBN          string `xml:"isbn"`
	LargeImageURL string `xml:"book_large_image_url"`
	ImageURL      string `xml:"book_image_url"`
	Description   string `xml:"book_description"`
	Published     string `xml:"book_published"`
	UserRating    string `xml:"user_rating"`
	AverageRating string `xml:"average_rating"`
	DateAdded     string `xml:"user_date_added"`
	ReadAt        string `xml:"user_read_at"`
}

type rssDoc struct {
	Items []rssItem `xml:"channel>item"`
}

// grDateLayouts are the (Ruby-ish) date formats Goodreads emits in RSS.
var grDateLayouts = []string{
	"Mon, 02 Jan 2006 15:04:05 -0700",
	"Mon Jan 02 15:04:05 -0700 2006",
	time.RFC1123Z,
}

func parseGRDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range grDateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseRSS decodes a Goodreads shelf RSS feed. encoding/xml resolves standard
// entities and does not expand external entities (safe by default).
func parseRSS(data []byte, shelf string) ([]sources.Book, error) {
	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make([]sources.Book, 0, len(doc.Items))
	for _, it := range doc.Items {
		id := strings.TrimSpace(it.BookID)
		if id == "" {
			continue // no identity — skip
		}
		cover := strings.TrimSpace(it.LargeImageURL)
		if cover == "" {
			cover = strings.TrimSpace(it.ImageURL)
		}
		year, _ := strconv.Atoi(strings.TrimSpace(it.Published))
		rating, _ := strconv.Atoi(strings.TrimSpace(it.UserRating))
		avg, _ := strconv.ParseFloat(strings.TrimSpace(it.AverageRating), 64)
		out = append(out, sources.Book{
			Source:        "goodreads",
			ExternalID:    id,
			Title:         strings.TrimSpace(it.Title),
			Author:        strings.TrimSpace(it.Author),
			ISBN10:        strings.TrimSpace(it.ISBN),
			Shelves:       []string{shelf},
			CoverURL:      upscaleGRCover(cover),
			Description:   strings.TrimSpace(it.Description),
			Year:          year,
			UserRating:    rating,
			AverageRating: avg,
			AddedAt:       parseGRDate(it.DateAdded),
			ReadAt:        parseGRDate(it.ReadAt),
		})
	}
	return out, nil
}
