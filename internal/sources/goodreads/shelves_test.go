package goodreads

import "testing"

const sidebarHTML = `<html><head><title>Rafa's books on Goodreads</title></head><body>
<div id="shelves">
<div class="userShelf"><a href="/review/list/123?shelf=to-read">to-read <span class="greyText">(121)</span></a></div>
<div class="userShelf"><a href="/review/list/123?shelf=read">read (1,340)</a></div>
<div class="userShelf"><a href="/review/list/123?shelf=currently-reading">currently-reading (2)</a></div>
<a href="/review/list/123?shelf=%23ALL%23">All (1463)</a>
</div>
<table><tbody id="booksBody"><tr><td><a href="/book/show/1">x</a></td></tr></tbody></table>
</body></html>`

func TestParseShelves(t *testing.T) {
	got, signedOut, err := parseShelves([]byte(sidebarHTML))
	if err != nil || signedOut {
		t.Fatalf("err=%v signedOut=%v", err, signedOut)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 shelves (skip %%23ALL%%23), got %d: %+v", len(got), got)
	}
	if got[0].Slug != "to-read" || got[0].Count != 121 {
		t.Fatalf("to-read: %+v", got[0])
	}
	if got[1].Slug != "read" || got[1].Count != 1340 {
		t.Fatalf("read count parse: %+v", got[1])
	}
}

func TestParseShelvesSignedOut(t *testing.T) {
	_, signedOut, err := parseShelves([]byte(`<html><head><title>Sign in</title></head><body></body></html>`))
	if err != nil || !signedOut {
		t.Fatalf("want signedOut=true, got %v %v", signedOut, err)
	}
}
