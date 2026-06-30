package main

import "testing"

func TestRunWebUnknownStillDispatches(t *testing.T) {
	// `web` must be a recognized subcommand (returns !=2 usage code with config error path)
	var out stringSink
	code := run([]string{"web"}, func(k string) string {
		return map[string]string{"SHELFARR_URL": "", "SHELFARR_TOKEN": ""}[k]
	}, &out)
	if code == 2 {
		t.Fatalf("`web` should be dispatched, not usage error; out=%s", out.String())
	}
}

type stringSink struct{ b []byte }

func (s *stringSink) Write(p []byte) (int, error) { s.b = append(s.b, p...); return len(p), nil }
func (s *stringSink) String() string              { return string(s.b) }
