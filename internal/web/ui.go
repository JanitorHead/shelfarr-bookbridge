package web

import "strings"

// stateClass maps a book/run state to a semantic color token used by the CSS
// (badge-<token> and accent-<token>). Keep in sync with style.css.
func stateClass(state string) string {
	switch state {
	case "done", "completed":
		return "ok"
	case "requesting", "requested", "searching", "downloading", "processing":
		return "active"
	case "not_found", "parked", "new":
		return "warn"
	case "failed", "cancelled":
		return "error"
	default: // baseline, ignored, …
		return "muted"
	}
}

// stateLabel humanizes a state for display.
func stateLabel(state string) string {
	switch state {
	case "not_found":
		return "not found"
	default:
		return strings.ReplaceAll(state, "_", " ")
	}
}

// activePage maps a request path to the nav key used to highlight the current tab.
func activePage(path string) string {
	switch {
	case path == "/":
		return "dashboard"
	case strings.HasPrefix(path, "/queue"):
		return "queue"
	case strings.HasPrefix(path, "/review"):
		return "review"
	case strings.HasPrefix(path, "/shelves"):
		return "shelves"
	case strings.HasPrefix(path, "/settings"):
		return "settings"
	default:
		return ""
	}
}

// list returns its arguments as a slice (templates can't build slice literals).
func list(xs ...string) []string { return xs }

// optLabel humanizes a <select> option value; unknown values pass through.
func optLabel(v string) string {
	switch v {
	case "":
		return "auto (use the cookie if one is set)"
	case "private_cookie":
		return "private — session cookie (private / >100-book shelves)"
	case "public_rss":
		return "public — RSS feed (public shelves, max 100)"
	default:
		return v
	}
}

// total sums all state counts (the size of the tracked library).
func total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// initials returns up to two leading letters of a title for the cover placeholder.
func initials(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	r := []rune(s)
	if len(r) >= 2 {
		return strings.ToUpper(string(r[:2]))
	}
	return strings.ToUpper(string(r[:1]))
}
