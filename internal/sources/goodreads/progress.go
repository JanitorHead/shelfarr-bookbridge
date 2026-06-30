package goodreads

import (
	"fmt"
	"regexp"
	"strconv"
)

// Goodreads reading progress lives in the user's status-updates feed
// (/user_status/list/<id>), NOT in the shelf table. Each update reads:
//
//	<span class="... user_status_header"> <a>User</a> is on page 357 of 416 of
//	  <a href=".../book/show/40624139-el-hambre-invisible">El Hambre Invisible</a>
//
// or, for %-tracked editions, "... is 45% done with <a .../book/show/ID>".
var statusPageRe = regexp.MustCompile(`on page (\d+) of (\d+) of\s*<a href="[^"]*?/book/show/(\d+)`)
var statusPctRe = regexp.MustCompile(`is (\d+)% done with\s*<a href="[^"]*?/book/show/(\d+)`)

type readingProgress struct {
	Pct   int
	Label string
}

// parseStatusUpdates maps book id -> latest reading progress from a Goodreads
// /user_status/list page. Updates render newest-first, so the first hit per book
// is the current progress.
func parseStatusUpdates(html []byte) map[string]readingProgress {
	out := map[string]readingProgress{}
	for _, m := range statusPageRe.FindAllSubmatch(html, -1) {
		page, _ := strconv.Atoi(string(m[1]))
		total, _ := strconv.Atoi(string(m[2]))
		id := string(m[3])
		if total <= 0 {
			continue
		}
		if _, seen := out[id]; seen {
			continue
		}
		pct := page * 100 / total
		if pct > 100 {
			pct = 100
		}
		out[id] = readingProgress{Pct: pct, Label: fmt.Sprintf("page %d of %d", page, total)}
	}
	for _, m := range statusPctRe.FindAllSubmatch(html, -1) {
		pct, _ := strconv.Atoi(string(m[1]))
		id := string(m[2])
		if _, seen := out[id]; seen {
			continue
		}
		if pct > 100 {
			pct = 100
		}
		out[id] = readingProgress{Pct: pct, Label: fmt.Sprintf("%d%% done", pct)}
	}
	return out
}
