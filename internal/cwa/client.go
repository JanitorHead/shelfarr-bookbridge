// Package cwa talks to a Calibre-Web-Automated instance over HTTP to tag books
// in the Calibre library (so a Goodreads shelf becomes a Calibre tag). It drives
// CWA's own session+CSRF endpoints — verified against a live CWA:
//
//	GET  /login                  -> csrf_token
//	POST /login                  -> session cookie (302 on success)
//	GET  /ajax/listbooks         -> {rows:[{id,title,authors,tags}]}
//	POST /ajax/editbooks/tags    -> {"success":true,...}  (form: pk,value,csrf_token)
package cwa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/PuerkitoBio/goquery"
)

var shelfIDRe = regexp.MustCompile(`^/shelf/(\d+)$`)

// shelfCountRe strips the " (N)" book-count suffix Calibre-Web appends to a
// shelf's sidebar label, leaving just the shelf name.
var shelfCountRe = regexp.MustCompile(`\s*\(\d+\)\s*$`)

// Book is a Calibre library entry as returned by /ajax/listbooks.
type Book struct {
	ID      int
	Title   string
	Authors string
	Tags    []string
}

type Client struct {
	base string
	user string
	pass config.SecretString
	hc   *http.Client
	csrf string
}

func New(base, user string, pass config.SecretString) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		base: strings.TrimRight(base, "/"),
		user: user, pass: pass,
		hc: &http.Client{Jar: jar, Timeout: 25 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (c *Client) csrfFrom(ctx context.Context, path string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}
	tok, _ := doc.Find(`input[name="csrf_token"]`).First().Attr("value")
	return tok, nil
}

// Login authenticates and captures a session-valid CSRF token for edits.
func (c *Client) Login(ctx context.Context) error {
	tok, err := c.csrfFrom(ctx, "/login")
	if err != nil {
		return fmt.Errorf("cannot reach CWA at %s: %w", c.base, err)
	}
	form := url.Values{"username": {c.user}, "password": {c.pass.Reveal()}, "next": {"/"}, "csrf_token": {tok}}
	req, _ := http.NewRequestWithContext(ctx, "POST", c.base+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound { // 302 = success; 200 = login page re-rendered
		return fmt.Errorf("CWA login failed — check the URL, username and password (HTTP %d)", resp.StatusCode)
	}
	c.csrf, err = c.csrfFrom(ctx, "/")
	if err != nil || c.csrf == "" {
		return fmt.Errorf("CWA: logged in but could not read a CSRF token")
	}
	return nil
}

// ListBooks returns every book in the library.
func (c *Client) ListBooks(ctx context.Context) ([]Book, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.base+"/ajax/listbooks?offset=0&limit=100000", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CWA listbooks HTTP %d", resp.StatusCode)
	}
	var d struct {
		Rows []struct {
			ID      int    `json:"id"`
			Title   string `json:"title"`
			Authors string `json:"authors"`
			Tags    string `json:"tags"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	out := make([]Book, 0, len(d.Rows))
	for _, r := range d.Rows {
		out = append(out, Book{ID: r.ID, Title: r.Title, Authors: r.Authors, Tags: SplitTags(r.Tags)})
	}
	return out, nil
}

// SetTags replaces a book's tag list.
func (c *Client) SetTags(ctx context.Context, id int, tags []string) error {
	form := url.Values{"pk": {strconv.Itoa(id)}, "value": {strings.Join(tags, ", ")}, "csrf_token": {c.csrf}}
	req, _ := http.NewRequestWithContext(ctx, "POST", c.base+"/ajax/editbooks/tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRFToken", c.csrf)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
	if resp.StatusCode != 200 {
		return fmt.Errorf("CWA set tags HTTP %d", resp.StatusCode)
	}
	if !strings.Contains(string(b), `"success"`) || strings.Contains(string(b), `"success": false`) {
		return fmt.Errorf("CWA rejected the tag edit: %s", strings.TrimSpace(string(b)))
	}
	return nil
}

// Shelves returns the user's Calibre-Web shelves as name->id (scraped from the
// sidebar, since CWA has no list-shelves API).
func (c *Client) Shelves(ctx context.Context) (map[string]int, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.base+"/", nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		m := shelfIDRe.FindStringSubmatch(strings.TrimPrefix(href, c.base))
		if m == nil {
			return
		}
		name := shelfCountRe.ReplaceAllString(strings.TrimSpace(a.Text()), "")
		if name != "" {
			id, _ := strconv.Atoi(m[1])
			out[name] = id
		}
	})
	return out, nil
}

// CreateShelf creates a (private) shelf and returns its id.
func (c *Client) CreateShelf(ctx context.Context, name string) (int, error) {
	form := url.Values{"title": {name}, "csrf_token": {c.csrf}}
	req, _ := http.NewRequestWithContext(ctx, "POST", c.base+"/shelf/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRFToken", c.csrf)
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "" {
		if m := shelfIDRe.FindStringSubmatch(strings.TrimPrefix(loc, c.base)); m != nil {
			id, _ := strconv.Atoi(m[1])
			return id, nil
		}
	}
	if sh, _ := c.Shelves(ctx); sh[name] != 0 { // fallback: re-scrape
		return sh[name], nil
	}
	return 0, fmt.Errorf("CWA: created shelf %q but could not resolve its id", name)
}

// AddToShelf adds a book to a shelf (idempotent on CWA's side).
func (c *Client) AddToShelf(ctx context.Context, shelfID, bookID int) error {
	req, _ := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/shelf/add/%d/%d", c.base, shelfID, bookID), nil)
	req.Header.Set("X-CSRFToken", c.csrf)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case 204, 200, 302:
		return nil
	default:
		return fmt.Errorf("CWA add-to-shelf HTTP %d", resp.StatusCode)
	}
}

// SetRating sets a book's rating on the 0–5 star scale (CWA stores 0–10 and
// doubles internally). 0 clears it.
func (c *Client) SetRating(ctx context.Context, id, stars int) error {
	if stars < 0 {
		stars = 0
	}
	if stars > 5 {
		stars = 5
	}
	return c.editParam(ctx, "rating", id, strconv.Itoa(stars))
}

// SetCustomColumn sets a Calibre custom column (col = the numeric column id, so
// "1" targets custom_column_1). Dates use "YYYY-MM-DD".
func (c *Client) SetCustomColumn(ctx context.Context, id int, col, value string) error {
	return c.editParam(ctx, "custom_column_"+col, id, value)
}

// editParam POSTs to /ajax/editbooks/<param> (form pk/value/csrf).
func (c *Client) editParam(ctx context.Context, param string, id int, value string) error {
	form := url.Values{"pk": {strconv.Itoa(id)}, "value": {value}, "csrf_token": {c.csrf}}
	req, _ := http.NewRequestWithContext(ctx, "POST", c.base+"/ajax/editbooks/"+param, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRFToken", c.csrf)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
	if resp.StatusCode != 200 {
		return fmt.Errorf("CWA edit %s HTTP %d", param, resp.StatusCode)
	}
	if strings.Contains(string(b), `"success": false`) {
		return fmt.Errorf("CWA edit %s rejected: %s", param, strings.TrimSpace(string(b)))
	}
	return nil
}

// readCbRe finds the "have read" checkbox on a book detail page; a standalone
// `checked` attribute (not data-checked) means the book is marked read.
var readCbRe = regexp.MustCompile(`<input[^>]*id="have_read_cb"[^>]*>`)
var readCheckedRe = regexp.MustCompile(`\schecked(\s|>|=)`)

// IsRead reports whether a Calibre book is marked read (Calibre-Web's native
// read flag), read from the book detail page.
func (c *Client) IsRead(ctx context.Context, id int) (bool, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/book/%d", c.base, id), nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("CWA book page HTTP %d", resp.StatusCode)
	}
	tag := readCbRe.Find(body)
	if tag == nil {
		return false, fmt.Errorf("CWA: no read control on book %d", id)
	}
	return readCheckedRe.Match(tag), nil
}

// MarkRead sets Calibre-Web's native read flag for a book. It is idempotent and
// only ever marks read (never un-reads): if the book is already read, it no-ops,
// so it never undoes a read state the user set manually.
func (c *Client) MarkRead(ctx context.Context, id int) error {
	read, err := c.IsRead(ctx, id)
	if err != nil {
		return err
	}
	if read {
		return nil
	}
	form := url.Values{"csrf_token": {c.csrf}}
	req, _ := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/ajax/toggleread/%d", c.base, id), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRFToken", c.csrf)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("CWA toggleread HTTP %d", resp.StatusCode)
	}
	return nil
}

// SplitTags parses CWA's comma-separated tag string.
func SplitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}
