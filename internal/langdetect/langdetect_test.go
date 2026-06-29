package langdetect

import "testing"

func TestDetect(t *testing.T) {
	d := New()
	if lang, ok := d.Detect("El nombre del viento"); !ok || lang != "es" {
		t.Fatalf("spanish title => %q,%v want es,true", lang, ok)
	}
	if lang, ok := d.Detect("The Name of the Wind"); !ok || lang != "en" {
		t.Fatalf("english title => %q,%v want en,true", lang, ok)
	}
	if _, ok := d.Detect(""); ok {
		t.Fatal("empty title must be ok=false")
	}
	if _, ok := d.Detect("a"); ok {
		t.Fatal("1-rune title must be ok=false")
	}
}
