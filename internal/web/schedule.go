package web

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

// composeSchedule turns the visual schedule controls (a preset, an optional
// HH:MM time for the daily preset, and a raw cron for the advanced preset) into
// a 5-field cron expression. An empty result means "no schedule" (off). Any
// non-empty expression is validated with cron.ParseStandard.
func composeSchedule(preset, timeHHMM, raw string) (string, error) {
	var expr string
	switch preset {
	case "off", "":
		return "", nil
	case "15min":
		expr = "*/15 * * * *"
	case "30min":
		expr = "*/30 * * * *"
	case "hourly":
		expr = "0 * * * *"
	case "6h":
		expr = "0 */6 * * *"
	case "daily":
		t := timeHHMM
		if t == "" {
			t = "00:00"
		}
		parts := strings.SplitN(t, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid time %q", timeHHMM)
		}
		hh, err := strconv.Atoi(parts[0])
		if err != nil || hh < 0 || hh > 23 {
			return "", fmt.Errorf("invalid hour in time %q", timeHHMM)
		}
		mm, err := strconv.Atoi(parts[1])
		if err != nil || mm < 0 || mm > 59 {
			return "", fmt.Errorf("invalid minute in time %q", timeHHMM)
		}
		expr = fmt.Sprintf("%d %d * * *", mm, hh)
	case "advanced":
		expr = raw
	default:
		return "", fmt.Errorf("unknown schedule preset %q", preset)
	}
	if expr == "" {
		return "", nil
	}
	if _, err := cron.ParseStandard(expr); err != nil {
		return "", err
	}
	return expr, nil
}

// cronToPreset reverses composeSchedule so the GUI can pre-select the matching
// preset (and HH:MM time for the daily case). Anything that doesn't match a
// known preset is surfaced as "advanced" so the raw expression stays editable.
func cronToPreset(expr string) (preset, timeHHMM string) {
	switch expr {
	case "":
		return "off", ""
	case "*/15 * * * *":
		return "15min", ""
	case "*/30 * * * *":
		return "30min", ""
	case "0 * * * *":
		return "hourly", ""
	case "0 */6 * * *":
		return "6h", ""
	}
	if fields := strings.Fields(expr); len(fields) == 5 &&
		fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		mm, errM := strconv.Atoi(fields[0])
		hh, errH := strconv.Atoi(fields[1])
		if errM == nil && errH == nil && mm >= 0 && mm < 60 && hh >= 0 && hh < 24 {
			return "daily", fmt.Sprintf("%02d:%02d", hh, mm)
		}
	}
	return "advanced", ""
}
