package goodreads

import "testing"

// real-shaped Goodreads Kindle-highlight markup (two highlights, one with a note).
const highlightsHTML = `<div>
<div class="js-readingNote" data-annotation-pair-id="a1|-0-|" data-book-id="44770129" data-has-note="false">
 <div class="noteHighlightContainer"><div class="noteHighlightContainer__location"> 30% </div>
 <div class="readingNoteContentContainer"><div class="noteHighlightTextContainer">
  <div class="noteHighlightTextContainer__highlightContainer"><div class="noteHighlightTextContainer__highlightText">
   <span id="freeTextContainer1">The easiest way to learn directly is to spend time doing the thing.</span></div></div></div></div></div></div>
<div class="js-readingNote" data-annotation-pair-id="a2|-0-|" data-book-id="44770129" data-has-note="true">
 <div class="noteHighlightContainer"><div class="noteHighlightContainer__location"> 33% </div>
 <div class="readingNoteContentContainer"><div class="noteHighlightTextContainer">
  <div class="noteHighlightTextContainer__highlightText"><span id="freeTextContainer2">Learn by doing.</span></div>
  <div class="noteHighlightTextContainer__noteContainer"><span>My note here</span></div></div></div></div></div>
</div>`

func TestParseHighlights(t *testing.T) {
	hs, err := parseHighlights([]byte(highlightsHTML))
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 {
		t.Fatalf("want 2 highlights, got %d", len(hs))
	}
	if hs[0].Location != "30%" || hs[0].Text != "The easiest way to learn directly is to spend time doing the thing." {
		t.Fatalf("hl0: %+v", hs[0])
	}
	if hs[1].Location != "33%" || hs[1].Note != "My note here" {
		t.Fatalf("hl1 note not captured: %+v", hs[1])
	}
}
