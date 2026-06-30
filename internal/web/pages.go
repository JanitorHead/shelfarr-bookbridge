package web

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

// chip is one filter pill: a label, the href that applies it (preserving the
// other active filters), an optional count, and whether it is currently active.
type chip struct {
	Label, Href string
	N           int
	On          bool
}

// libHref renders a Library URL from a filter, omitting empty fields.
func libHref(f store.LibraryFilter) string {
	v := url.Values{}
	if f.State != "" {
		v.Set("state", f.State)
	}
	if f.Status != "" {
		v.Set("status", f.Status)
	}
	if f.Tag != "" {
		v.Set("tag", f.Tag)
	}
	if f.Owned != "" {
		v.Set("owned", f.Owned)
	}
	if f.Q != "" {
		v.Set("q", f.Q)
	}
	if len(v) == 0 {
		return "/queue"
	}
	return "/queue?" + v.Encode()
}

// withField clones a filter overriding exactly one field, for chip hrefs that
// keep every other active filter intact.
func withField(f store.LibraryFilter, field, val string) store.LibraryFilter {
	switch field {
	case "status":
		f.Status = val
	case "tag":
		f.Tag = val
	case "owned":
		f.Owned = val
	}
	return f
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	f := store.LibraryFilter{
		State:  r.URL.Query().Get("state"),
		Status: r.URL.Query().Get("status"),
		Tag:    r.URL.Query().Get("tag"),
		Owned:  r.URL.Query().Get("owned"),
		Q:      r.URL.Query().Get("q"),
		Limit:  1000,
	}
	rows, _ := s.st.ListLibrary(ctx, f)
	statusCounts, _ := s.st.ReadingStatusCounts(ctx)
	tagCounts, _ := s.st.TopicTagCounts(ctx)
	owned, _ := s.st.OwnedCount(ctx)

	// Reading-status chips (All + each status), each preserving the other filters.
	statusChips := []chip{{Label: "All", Href: libHref(withField(f, "status", "")), On: f.Status == ""}}
	for _, st := range []string{"to_read", "reading", "read", "dnf"} {
		statusChips = append(statusChips, chip{
			Label: readingLabel(st), N: statusCounts[st], On: f.Status == st,
			Href: libHref(withField(f, "status", st)),
		})
	}
	// Ownership chips.
	ownChips := []chip{
		{Label: "All", Href: libHref(withField(f, "owned", "")), On: f.Owned == ""},
		{Label: "Owned", N: owned, Href: libHref(withField(f, "owned", "owned")), On: f.Owned == "owned"},
		{Label: "Not owned", Href: libHref(withField(f, "owned", "missing")), On: f.Owned == "missing"},
	}
	// Topic-tag chips, sorted for stable order; clicking the active tag clears it.
	slugs := make([]string, 0, len(tagCounts))
	for slug := range tagCounts {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	tagChips := make([]chip, 0, len(slugs))
	for _, slug := range slugs {
		next := slug
		if f.Tag == slug {
			next = ""
		}
		tagChips = append(tagChips, chip{
			Label: slug, N: tagCounts[slug], On: f.Tag == slug,
			Href: libHref(withField(f, "tag", next)),
		})
	}

	s.render(w, r, "queue", "Library", map[string]any{
		"Rows": rows, "Shown": len(rows),
		"Q": f.Q, "State": f.State, "Status": f.Status, "Tag": f.Tag, "Owned": f.Owned,
		"StatusChips": statusChips, "OwnChips": ownChips, "TagChips": tagChips,
		"HasTags": len(tagChips) > 0,
	})
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		src, id := r.PostFormValue("source"), r.PostFormValue("external_id")
		switch r.PostFormValue("action") {
		case "ignore":
			s.st.IgnoreBook(ctx, src, id)
		case "retry":
			s.st.RetryBook(ctx, src, id)
		}
		http.Redirect(w, r, "/review", http.StatusSeeOther)
		return
	}
	nf, _ := s.st.ListBooks(ctx, "not_found", "", 500)
	parked, _ := s.st.ListBooks(ctx, "parked", "", 500)
	s.render(w, r, "review", "Review", map[string]any{"Rows": append(nf, parked...)})
}

func (s *Server) handleShelves(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	cfg := s.cfg()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		// Save every shelf row carried in the form (the "shelves" hidden field
		// lists the slugs) so all toggles persist in one click.
		for _, sh := range strings.Split(r.PostFormValue("shelves"), ",") {
			if sh == "" {
				continue
			}
			enabled := r.PostFormValue("enabled_"+sh) != ""
			s.st.SetShelfConfig(ctx, sh, enabled, r.PostFormValue("format_"+sh), r.PostFormValue("language_"+sh))
		}
		http.Redirect(w, r, "/shelves?saved=1", http.StatusSeeOther)
		return
	}
	// Prefer discovered/configured shelves; fall back to the legacy SHELVES text
	// so existing installs still see their shelves until they hit Refresh.
	rows, _ := s.st.AllShelfConfigs(ctx)
	if len(rows) == 0 {
		for _, sh := range cfg.Shelves {
			rows = append(rows, store.ShelfCfg{Shelf: sh, Name: sh, Enabled: true})
		}
	}
	slugs := make([]string, 0, len(rows))
	for _, c := range rows {
		slugs = append(slugs, c.Shelf)
	}
	sourceName := "Goodreads"
	if cfg.Source == "hardcover" {
		sourceName = "Hardcover"
	}
	s.render(w, r, "shelves", "Shelves", map[string]any{
		"Shelves": rows, "Slugs": strings.Join(slugs, ","),
		"CanDiscover": s.discover != nil, "Source": sourceName,
		"Refreshed": r.URL.Query().Get("refreshed"), "Saved": r.URL.Query().Get("saved") != "",
		"Err": r.URL.Query().Get("err"),
	})
}

// handleShelvesRefresh asks the connected source to enumerate the user's shelves
// and records them (disabled by default) so they appear as toggles.
func (s *Server) handleShelvesRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/shelves", http.StatusSeeOther)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	if s.discover == nil {
		http.Redirect(w, r, "/shelves?err="+url.QueryEscape("shelf discovery is not available"), http.StatusSeeOther)
		return
	}
	shelves, err := s.discover(ctx)
	if err != nil {
		http.Redirect(w, r, "/shelves?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	for _, sh := range shelves {
		_ = s.st.UpsertDiscoveredShelf(ctx, sh.Slug, sh.Name, sh.Count)
	}
	http.Redirect(w, r, "/shelves?refreshed="+strconv.Itoa(len(shelves)), http.StatusSeeOther)
}
