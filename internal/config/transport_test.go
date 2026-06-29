package config

import "testing"

func TestCheckTransport(t *testing.T) {
	cases := []struct {
		url      string
		insecure bool
		wantErr  bool
	}{
		{"https://shelfarr.example", false, false},
		{"http://127.0.0.1:3000", false, false},
		{"http://localhost:3000", false, false},
		{"http://192.168.1.5:3000", false, true},
		{"http://192.168.1.5:3000", true, false},
		{"://bad", false, true},
	}
	for _, c := range cases {
		err := CheckTransport(c.url, c.insecure)
		if (err != nil) != c.wantErr {
			t.Errorf("CheckTransport(%q, %v) err=%v wantErr=%v", c.url, c.insecure, err, c.wantErr)
		}
	}
}
