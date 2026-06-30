package goodreads

import "testing"

// real-shaped Goodreads status-updates markup (newest-first): the same book has
// two updates; the latest (page 357) must win.
const statusHTML = `
<div id="user_status_358169720">
 <span class="uitext greyText inlineblock stacked user_status_header">
   <a href="/user/show/64250839-rafa">Rafa</a> is on page 357 of 416 of
   <a href="https://www.goodreads.com/book/show/40624139-el-hambre-invisible" rel="nofollow noopener">El Hambre Invisible</a>
 </span>
</div>
<div id="user_status_358000001">
 <span class="user_status_header">
   <a href="/user/show/64250839-rafa">Rafa</a> is on page 177 of 416 of
   <a href="https://www.goodreads.com/book/show/40624139-el-hambre-invisible">El Hambre Invisible</a>
 </span>
</div>
<div id="user_status_357000002">
 <span class="user_status_header">
   <a href="/user/show/64250839-rafa">Rafa</a> is 45% done with
   <a href="https://www.goodreads.com/book/show/31307672-how-to-be-everything">How to Be Everything</a>
 </span>
</div>`

func TestParseStatusUpdates(t *testing.T) {
	got := parseStatusUpdates([]byte(statusHTML))
	if len(got) != 2 {
		t.Fatalf("want 2 books, got %d: %+v", len(got), got)
	}
	p := got["40624139"]
	if p.Pct != 85 || p.Label != "page 357 of 416" { // 357/416 = 85%, latest wins
		t.Fatalf("El Hambre Invisible: got %+v", p)
	}
	q := got["31307672"]
	if q.Pct != 45 || q.Label != "45% done" {
		t.Fatalf("How to Be Everything: got %+v", q)
	}
}
