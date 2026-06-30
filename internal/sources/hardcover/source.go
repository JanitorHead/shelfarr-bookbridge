// Package hardcover reads a user's books from Hardcover (https://hardcover.app)
// via its public GraphQL API, as an alternative ingest source to Goodreads.
package hardcover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// ErrTokenInvalid is returned when Hardcover rejects the API token.
var ErrTokenInvalid = errors.New("hardcover rejected the API token — grab a fresh one at hardcover.app → account → API")

const defaultBase = "https://api.hardcover.app/v1/graphql"

// statusByShelf maps our shelf slugs to Hardcover's status_id buckets.
var statusByShelf = map[string]int{
	"want-to-read":      1,
	"currently-reading": 2,
	"read":              3,
}

type Source struct {
	token config.SecretString
	base  string
	hc    *http.Client
}

func NewSource(token config.SecretString, base string, hc *http.Client) *Source {
	if base == "" {
		base = defaultBase
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Source{token: token, base: base, hc: hc}
}

// ListShelves returns Hardcover's reading-status buckets as toggleable shelves.
// (These are fixed, so they appear even before a live query is made.)
func (s *Source) ListShelves(ctx context.Context) ([]sources.Shelf, error) {
	return []sources.Shelf{
		{Slug: "want-to-read", Name: "Want to Read"},
		{Slug: "currently-reading", Name: "Currently Reading"},
		{Slug: "read", Name: "Read"},
	}, nil
}

// Fetch returns the books in the selected status shelves.
func (s *Source) Fetch(ctx context.Context, shelves []string) ([]sources.Book, error) {
	var all []sources.Book
	seen := map[string]bool{}
	for _, shelf := range shelves {
		status, ok := statusByShelf[shelf]
		if !ok {
			continue // unknown shelf slug — skip
		}
		books, err := s.fetchStatus(ctx, shelf, status)
		if err != nil {
			return nil, err
		}
		for _, b := range books {
			if seen[b.ExternalID] {
				continue
			}
			seen[b.ExternalID] = true
			all = append(all, b)
		}
	}
	return all, nil
}

// userBooksQuery pulls the full personal record per book: the user's rating,
// reading dates, and progress (latest read first), plus book metadata. A single
// wrong field name fails the whole query, so this is validated live before ship.
const userBooksQuery = `query($status: Int!) {
  me {
    user_books(where: {status_id: {_eq: $status}}, limit: 1000) {
      rating
      status_id
      user_book_reads(order_by: {started_at: desc_nulls_last}) {
        started_at
        finished_at
        progress
        progress_pages
        edition { pages }
      }
      book {
        id
        title
        release_year
        pages
        description
        contributions { author { name } }
        image { url }
      }
    }
  }
}`

func (s *Source) fetchStatus(ctx context.Context, shelf string, status int) ([]sources.Book, error) {
	body, err := s.do(ctx, userBooksQuery, map[string]any{"status": status})
	if err != nil {
		return nil, err
	}
	return parseUserBooks(body, shelf)
}

// do executes a GraphQL request and returns the raw response body.
func (s *Source) do(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, "POST", s.base, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token.Reveal())
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hardcover request: %w", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, ErrTokenInvalid
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("hardcover HTTP %d", resp.StatusCode)
	}
	return b, nil
}

// hcDateLayouts are the date formats Hardcover returns for read dates.
var hcDateLayouts = []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05"}

func hcDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, l := range hcDateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseUserBooks maps a Hardcover user_books GraphQL response into Books,
// including the user's rating, reading dates, and progress.
func parseUserBooks(data []byte, shelf string) ([]sources.Book, error) {
	var resp struct {
		Data struct {
			Me []struct {
				UserBooks []struct {
					Rating        float64 `json:"rating"`
					StatusID      int     `json:"status_id"`
					UserBookReads []struct {
						StartedAt     string  `json:"started_at"`
						FinishedAt    string  `json:"finished_at"`
						Progress      float64 `json:"progress"`
						ProgressPages int     `json:"progress_pages"`
						Edition       struct {
							Pages int `json:"pages"`
						} `json:"edition"`
					} `json:"user_book_reads"`
					Book struct {
						ID            int    `json:"id"`
						Title         string `json:"title"`
						ReleaseYear   int    `json:"release_year"`
						Pages         int    `json:"pages"`
						Description   string `json:"description"`
						Contributions []struct {
							Author struct {
								Name string `json:"name"`
							} `json:"author"`
						} `json:"contributions"`
						Image struct {
							URL string `json:"url"`
						} `json:"image"`
					} `json:"book"`
				} `json:"user_books"`
			} `json:"me"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("hardcover decode: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("hardcover API: %s", resp.Errors[0].Message)
	}
	var out []sources.Book
	for _, me := range resp.Data.Me {
		for _, ub := range me.UserBooks {
			bk := ub.Book
			if bk.ID == 0 || bk.Title == "" {
				continue
			}
			author := ""
			if len(bk.Contributions) > 0 {
				author = bk.Contributions[0].Author.Name
			}
			b := sources.Book{
				Source:      "hardcover",
				ExternalID:  strconv.Itoa(bk.ID),
				Title:       bk.Title,
				Author:      author,
				Year:        bk.ReleaseYear,
				Description: bk.Description,
				CoverURL:    bk.Image.URL,
				UserRating:  int(ub.Rating + 0.5), // round 0–5 stars to our int field
				Shelves:     []string{shelf},
			}
			// The reads are ordered newest-first; the first one carries the most
			// recent dates and progress.
			if len(ub.UserBookReads) > 0 {
				r := ub.UserBookReads[0]
				b.StartedAt = hcDate(r.StartedAt)
				b.ReadAt = hcDate(r.FinishedAt)
				pages := r.Edition.Pages
				if pages == 0 {
					pages = bk.Pages
				}
				b.ProgressPct = readingPct(r.Progress, r.ProgressPages, pages)
				if r.ProgressPages > 0 && pages > 0 {
					b.ProgressLabel = fmt.Sprintf("page %d of %d", r.ProgressPages, pages)
				}
			}
			out = append(out, b)
		}
	}
	return out, nil
}

// readingPct resolves a 0–100 progress percentage: prefer Hardcover's own
// progress value, else derive it from pages read over total pages.
func readingPct(progress float64, pagesRead, totalPages int) int {
	if progress > 0 {
		if progress > 100 {
			progress = 100
		}
		return int(progress + 0.5)
	}
	if pagesRead > 0 && totalPages > 0 {
		pct := float64(pagesRead) / float64(totalPages) * 100
		if pct > 100 {
			pct = 100
		}
		return int(pct + 0.5)
	}
	return 0
}
