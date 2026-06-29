package langdetect

import (
	"strings"

	"github.com/pemistahl/lingua-go"
)

// Detector wraps lingua restricted to English+Spanish, gating on confidence.
type Detector struct{ d lingua.LanguageDetector }

func New() *Detector {
	d := lingua.NewLanguageDetectorBuilder().
		FromLanguages(lingua.English, lingua.Spanish).
		WithPreloadedLanguageModels().
		Build()
	return &Detector{d: d}
}

// Detect returns an ISO 639-1 code only when the top language scores >= 0.65 and
// beats the runner-up by >= 0.15; otherwise ok=false (omit the language).
func (x *Detector) Detect(title string) (string, bool) {
	t := strings.TrimSpace(title)
	if len([]rune(t)) < 2 {
		return "", false
	}
	vals := x.d.ComputeLanguageConfidenceValues(t)
	if len(vals) < 2 {
		return "", false
	}
	top, second := vals[0], vals[1]
	if top.Value() < 0.65 || top.Value()-second.Value() < 0.15 {
		return "", false
	}
	return strings.ToLower(top.Language().IsoCode639_1().String()), true
}
