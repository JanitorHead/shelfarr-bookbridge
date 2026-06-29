package resolver

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalize lowercases, strips accents and non-alphanumerics, collapses spaces.
func normalize(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, _ := transform.String(t, s)
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(out) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case !prevSpace:
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func trigrams(s string) map[string]struct{} {
	s = "  " + s + "  "
	m := make(map[string]struct{})
	r := []rune(s)
	for i := 0; i+3 <= len(r); i++ {
		m[string(r[i:i+3])] = struct{}{}
	}
	return m
}

// mainTitle returns the part before a subtitle separator (":"), e.g.
// "Thinking In Systems: A Primer" -> "Thinking In Systems".
func mainTitle(s string) string {
	if i := strings.IndexByte(s, ':'); i > 0 {
		return s[:i]
	}
	return s
}

// TitleSimilarity is subtitle-tolerant: it returns the better of the full-title
// and main-title (pre-":") trigram similarity. This matches "Foo: A Primer"
// against "Foo" (metadata sources often drop the subtitle) without letting
// "Dune" match "Dune Messiah" (neither has a subtitle, so full == main is low).
func TitleSimilarity(a, b string) float64 {
	full := Similarity(a, b)
	if m := Similarity(mainTitle(a), mainTitle(b)); m > full {
		return m
	}
	return full
}

// Similarity is the Dice coefficient of character trigrams after normalization.
func Similarity(a, b string) float64 {
	na, nb := normalize(a), normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	ta, tb := trigrams(na), trigrams(nb)
	inter := 0
	for g := range ta {
		if _, ok := tb[g]; ok {
			inter++
		}
	}
	return 2 * float64(inter) / float64(len(ta)+len(tb))
}
