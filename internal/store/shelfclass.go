package store

import "strings"

// statusShelves maps reading-STATUS shelf slugs (Goodreads + Hardcover) to a
// normalized reading status. Every other shelf is a topic tag.
var statusShelves = map[string]string{
	"to-read":           "to_read",
	"want-to-read":      "to_read", // Hardcover
	"currently-reading": "reading",
	"reading":           "reading",
	"read":              "read",
	"did-not-finish":    "dnf",
	"dnf":               "dnf",
}

// IsStatusShelf reports whether a shelf slug represents reading status (not a tag).
func IsStatusShelf(slug string) bool {
	_, ok := statusShelves[strings.ToLower(strings.TrimSpace(slug))]
	return ok
}

// ReadingStatusFor derives one reading status from a book's shelves, preferring
// the "most advanced" state (read > dnf > reading > to_read).
func ReadingStatusFor(shelves []string) string {
	rank := map[string]int{"read": 4, "dnf": 3, "reading": 2, "to_read": 1}
	best, bestRank := "", 0
	for _, sh := range shelves {
		if st, ok := statusShelves[strings.ToLower(strings.TrimSpace(sh))]; ok && rank[st] > bestRank {
			best, bestRank = st, rank[st]
		}
	}
	return best
}

// TopicTags returns only the non-status (topic) shelves of a book.
func TopicTags(shelves []string) []string {
	var out []string
	for _, sh := range shelves {
		if !IsStatusShelf(sh) {
			out = append(out, sh)
		}
	}
	return out
}
