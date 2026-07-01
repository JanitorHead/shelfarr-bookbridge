package goodreads

import "testing"

const sampleHTML = `<html><head><title>Rafa's books</title></head><body>
<table><tbody id="booksBody">
<tr id="review_1">
 <td class="field title"><div class="value"><a href="/book/show/12345.El_Nombre_del_Viento" title="El Nombre del Viento">El Nombre del Viento</a></div></td>
 <td class="field author"><div class="value"><a href="/author/show/1.x">Rothfuss, Patrick</a></div></td>
 <td class="field isbn"><div class="value">8401352835</div></td>
 <td class="field rating"><div class="value"><div class="stars" data-rating="4"></div></div></td>
 <td class="field date_started"><div class="value"><div class="editable_date"><span class="date_started_value">Jan 05, 2024</span></div></div></td>
 <td class="field date_read"><div class="value"><div class="date_row"><span class="date_read_value">Feb 20, 2024</span><span class="date_read_value">Jan 01, 2020</span></div></div></td>
 <td class="field date_added"><div class="value"><span title="December 01, 2023"> Dec 01, 2023 </span></div></td>
</tr>
<tr id="review_2">
 <td class="field title"><div class="value"><a href="/book/show/67890.The_Wise_Mans_Fear" title="The Wise Man's Fear">The Wise Man&#39;s Fear</a></div></td>
 <td class="field author"><div class="value"><a href="/author/show/1.x">Rothfuss, Patrick</a></div></td>
 <td class="field isbn"><div class="value"></div></td>
</tr>
</tbody></table></body></html>`

const signinHTML = `<html><head><title>Sign in</title></head><body><div id="choices">sign in</div></body></html>`

func TestParseHTMLList(t *testing.T) {
	books, signedOut, err := parseHTMLList([]byte(sampleHTML), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	if signedOut {
		t.Fatal("should not be signed out")
	}
	if len(books) != 2 {
		t.Fatalf("want 2 books, got %d", len(books))
	}
	if books[0].ExternalID != "12345" || books[0].Source != "goodreads" {
		t.Fatalf("bad id: %+v", books[0])
	}
	if books[0].Title != "El Nombre del Viento" {
		t.Fatalf("bad title: %q", books[0].Title)
	}
	if books[1].ISBN10 != "" {
		t.Fatalf("empty isbn should stay empty, got %q", books[1].ISBN10)
	}
	if books[0].Shelves[0] != "to-read" {
		t.Fatalf("shelf not tagged: %v", books[0].Shelves)
	}
	if books[0].UserRating != 4 {
		t.Fatalf("rating: got %d want 4 (from div.stars data-rating)", books[0].UserRating)
	}
	if got := books[0].StartedAt.Format("2006-01-02"); got != "2024-01-05" {
		t.Fatalf("started_at: got %q", got)
	}
	if got := books[0].ReadAt.Format("2006-01-02"); got != "2024-02-20" {
		t.Fatalf("read_at: got %q", got)
	}
	if got := books[0].AddedAt.Format("2006-01-02"); got != "2023-12-01" {
		t.Fatalf("added_at: got %q", got)
	}
	if !books[1].StartedAt.IsZero() {
		t.Fatalf("book with no dates should have zero StartedAt, got %v", books[1].StartedAt)
	}
}

func TestParseHTMLListDetectsSignIn(t *testing.T) {
	_, signedOut, err := parseHTMLList([]byte(signinHTML), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	if !signedOut {
		t.Fatal("expected signedOut=true for sign-in page")
	}
}
