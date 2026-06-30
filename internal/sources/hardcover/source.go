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

const userBooksQuery = `query($status: Int!) {
  me {
    user_books(where: {status_id: {_eq: $status}}, limit: 1000) {
      book { id title release_year contributions { author { name } } }
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

// parseUserBooks maps a Hardcover user_books GraphQL response into Books.
func parseUserBooks(data []byte, shelf string) ([]sources.Book, error) {
	var resp struct {
		Data struct {
			Me []struct {
				UserBooks []struct {
					Book struct {
						ID            int    `json:"id"`
						Title         string `json:"title"`
						ReleaseYear   int    `json:"release_year"`
						Contributions []struct {
							Author struct {
								Name string `json:"name"`
							} `json:"author"`
						} `json:"contributions"`
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
			out = append(out, sources.Book{
				Source:     "hardcover",
				ExternalID: strconv.Itoa(bk.ID),
				Title:      bk.Title,
				Author:     author,
				Year:       bk.ReleaseYear,
				Shelves:    []string{shelf},
			})
		}
	}
	return out, nil
}
