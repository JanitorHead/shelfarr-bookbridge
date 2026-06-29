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
