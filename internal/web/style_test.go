package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoInlineStylesInTemplates(t *testing.T) {
	dir := "templates"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "style=\"") {
			t.Errorf("%s contains an inline style= attribute; move it to style.css (CSP blocks inline styles)", e.Name())
		}
	}
}

func TestStylesheetHasComponents(t *testing.T) {
	b, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".banner-error", ".btn-secondary", ".badge-done"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("style.css missing %s", want)
		}
	}
}
