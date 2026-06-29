package resolver

import "testing"

func TestSimilarity(t *testing.T) {
	if s := Similarity("Dune", "Dune"); s < 0.999 {
		t.Fatalf("identical should be 1.0, got %f", s)
	}
	if s := Similarity("The Name of the Wind", "Name of the Wind"); s < 0.7 {
		t.Fatalf("close titles should score high, got %f", s)
	}
	if s := Similarity("Dune", "War and Peace"); s > 0.2 {
		t.Fatalf("unrelated should score low, got %f", s)
	}
	// normalization: case + accents + punctuation ignored
	if s := Similarity("El Nombre del Viento", "el nombre del viento!"); s < 0.95 {
		t.Fatalf("normalization failed, got %f", s)
	}
}
