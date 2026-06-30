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
	"strconv"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/PuerkitoBio/goquery"
)

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
