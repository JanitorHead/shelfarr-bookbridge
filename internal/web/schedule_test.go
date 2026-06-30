package web

import "testing"

func TestComposeSchedule(t *testing.T) {
	cases := []struct {
		preset, timeHHMM, raw string
		want                  string
		wantErr               bool
	}{
		{"off", "", "", "", false},
		{"15min", "", "", "*/15 * * * *", false},
		{"30min", "", "", "*/30 * * * *", false},
		{"hourly", "", "", "0 * * * *", false},
		{"6h", "", "", "0 */6 * * *", false},
		{"daily", "06:30", "", "30 6 * * *", false},
		{"advanced", "", "*/5 * * * *", "*/5 * * * *", false},
		{"advanced", "", "not a cron", "", true},
	}
	for _, c := range cases {
		got, err := composeSchedule(c.preset, c.timeHHMM, c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("composeSchedule(%q,%q,%q) expected error, got %q", c.preset, c.timeHHMM, c.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("composeSchedule(%q,%q,%q) unexpected error: %v", c.preset, c.timeHHMM, c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("composeSchedule(%q,%q,%q)=%q, want %q", c.preset, c.timeHHMM, c.raw, got, c.want)
		}
	}
}

func TestCronToPreset(t *testing.T) {
	cases := []struct {
		expr             string
		preset, timeHHMM string
	}{
		{"", "off", ""},
		{"*/15 * * * *", "15min", ""},
		{"*/30 * * * *", "30min", ""},
		{"0 * * * *", "hourly", ""},
		{"0 */6 * * *", "6h", ""},
		{"30 6 * * *", "daily", "06:30"},
		{"*/5 * * * *", "advanced", ""},
	}
	for _, c := range cases {
		preset, timeHHMM := cronToPreset(c.expr)
		if preset != c.preset || timeHHMM != c.timeHHMM {
			t.Errorf("cronToPreset(%q)=(%q,%q), want (%q,%q)", c.expr, preset, timeHHMM, c.preset, c.timeHHMM)
		}
	}
}
