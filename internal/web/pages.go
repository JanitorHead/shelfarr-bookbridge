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
		return "/"
	}
	return "/?" + v.Encode()
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

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
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

	// View mode (grid | list), default grid, remembered in a cookie. When passed
	// as ?view= it's persisted; otherwise read from the cookie.
	view := r.URL.Query().Get("view")
	if view == "grid" || view == "list" {
		http.SetCookie(w, &http.Cookie{Name: "bb_view", Value: view, Path: "/", MaxAge: 31536000, SameSite: http.SameSiteLaxMode})
	} else if c, err := r.Cookie("bb_view"); err == nil && (c.Value == "grid" || c.Value == "list") {
		view = c.Value
	} else {
		view = "grid"
	}
	base := libHref(f)
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}

	s.render(w, r, "queue", "Library", map[string]any{
		"Rows": rows, "Shown": len(rows),
		"Q": f.Q, "State": f.State, "Status": f.Status, "Tag": f.Tag, "Owned": f.Owned,
		"StatusChips": statusChips, "OwnChips": ownChips, "TagChips": tagChips,
		"HasTags": len(tagChips) > 0, "CWAOn": s.cfg().CWAConfigured(),
		"OwnRefreshed": r.URL.Query().Get("owned_refreshed") != "",
		"Err":          r.URL.Query().Get("err"),
		"View":         view, "GridHref": base + sep + "view=grid", "ListHref": base + sep + "view=list",
	})
}

// handleBook renders one book's detail. With ?drawer=1 it returns just the
// partial (for the slide-in drawer, fetched by app.js); otherwise a full page
// (the no-JS fallback). Path: /book/<source>/<externalID>.
func (s *Server) handleBook(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/book/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	source, id := parts[0], parts[1]
	d, err := s.st.BookDetail(context.Background(), source, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cfg := s.cfg()
	goodreadsURL := ""
	if source == "goodreads" {
		goodreadsURL = "https://www.goodreads.com/book/show/" + id
	}
	calibreURL := ""
	if d.OwnedInCWA && d.CalibreID > 0 && cfg.CWAURL != "" {
		calibreURL = strings.TrimRight(cfg.CWAURL, "/") + "/book/" + itoa(d.CalibreID)
	}
	csrf := ""
	if se := s.session(r); se != nil {
		csrf = se.csrf
	}
	data := map[string]any{
		"B": d, "Own": ownership(d.BookRow), "Topics": store.TopicTags(d.Shelves),
		"GoodreadsURL": goodreadsURL, "CalibreURL": calibreURL, "CSRF": csrf,
	}
	if r.URL.Query().Get("drawer") != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		// Execute a CLONE, never s.tmpl itself: executing the shared template makes
		// it un-Clone-able, which would break every other page's render().
		if t, err := s.tmpl.Clone(); err == nil {
			_ = t.ExecuteTemplate(w, "bookdetail", data)
		}
		return
	}
	s.render(w, r, "detail", d.Title, data)
}

// handleRefreshOwnership re-runs the CWA ownership cross-reference on demand.
func (s *Server) handleRefreshOwnership(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	if s.refreshOwn == nil {
		http.Redirect(w, r, "/?err="+url.QueryEscape("CWA is not configured — set it in Settings"), http.StatusSeeOther)
		return
	}
	if err := s.refreshOwn(context.Background()); err != nil {
		http.Redirect(w, r, "/?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?owned_refreshed=1", http.StatusSeeOther)
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
		http.Redirect(w, r, "/activity", http.StatusSeeOther)
		return
	}
	// Review is merged into Activity; the GET just redirects there.
	http.Redirect(w, r, "/activity", http.StatusMovedPermanently)
}

// shelvesVM builds the shelf-management view-model embedded in the Settings page.
func (s *Server) shelvesVM(r *http.Request) map[string]any {
	ctx := context.Background()
	cfg := s.cfg()
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
	return map[string]any{
		"Shelves": rows, "Slugs": strings.Join(slugs, ","),
		"CanDiscover": s.discover != nil, "Source": sourceName,
		"Refreshed": r.URL.Query().Get("refreshed"), "Saved": r.URL.Query().Get("saved") != "",
		"Err": r.URL.Query().Get("err"),
	}
}

// handleShelves saves the shelf toggles (POST). Shelf management now lives inside
// the Settings page, so GET redirects there.
func (s *Server) handleShelves(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/settings#shelves", http.StatusMovedPermanently)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	r.ParseForm()
	for _, sh := range strings.Split(r.PostFormValue("shelves"), ",") {
		if sh == "" {
			continue
		}
		enabled := r.PostFormValue("enabled_"+sh) != ""
		s.st.SetShelfConfig(ctx, sh, enabled, r.PostFormValue("format_"+sh), r.PostFormValue("language_"+sh))
	}
	http.Redirect(w, r, "/settings?saved=1#shelves", http.StatusSeeOther)
}

// handleShelvesRefresh asks the connected source to enumerate the user's shelves
// and records them (disabled by default) so they appear as toggles.
func (s *Server) handleShelvesRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/settings#shelves", http.StatusSeeOther)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	if s.discover == nil {
		http.Redirect(w, r, "/settings?err="+url.QueryEscape("shelf discovery is not available"), http.StatusSeeOther)
		return
	}
	shelves, err := s.discover(ctx)
	if err != nil {
		http.Redirect(w, r, "/settings?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	for _, sh := range shelves {
		_ = s.st.UpsertDiscoveredShelf(ctx, sh.Slug, sh.Name, sh.Count)
	}
	http.Redirect(w, r, "/settings?refreshed="+strconv.Itoa(len(shelves)), http.StatusSeeOther)
}
