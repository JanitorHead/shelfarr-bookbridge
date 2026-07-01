package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ShelfarrURL          string
	ShelfarrToken        SecretString
	Source               string // "goodreads" (default) | "hardcover"
	GoodreadsUserID      string
	GoodreadsFeedKey     SecretString
	GoodreadsCookie      SecretString
	GoodreadsMode        string
	HardcoverToken       SecretString
	HardcoverUsername    string
	Shelves              []string
	Format               string
	SimilarityThreshold  float64
	FirstRun             string
	MaxRequestsPerRun    int
	LangInference        bool
	ShelfarrInsecure     bool
	ShelfarrAutoRetry    bool // re-poke stuck (failed/attention) Shelfarr requests
	ShelfarrAutoRetryMax int  // max auto-retries per request
	Schedule             string
	CWAEnabled           bool
	CWAURL               string
	CWAUsername          string
	CWAPassword          SecretString
	CWADateColumn        string // Calibre custom-column id for "date added" (e.g. "1"); "" = off
	GUIPort              string
	GUIBind              string
	AuthMethod           string
	AuthRequired         string
}

func Load() (Config, error) { return loadFrom(os.Getenv) }

// Load2 loads config from a custom getenv (used by the CLI/tests).
func Load2(get func(string) string) (Config, error) { return loadFrom(get) }

func loadFrom(get func(string) string) (Config, error) {
	c := Config{
		ShelfarrURL:          get("SHELFARR_URL"),
		ShelfarrToken:        SecretString(get("SHELFARR_TOKEN")),
		Source:               orDefault(get("SOURCE"), "goodreads"),
		GoodreadsUserID:      get("GOODREADS_USER_ID"),
		GoodreadsFeedKey:     SecretString(get("GOODREADS_FEED_KEY")),
		GoodreadsCookie:      SecretString(get("GOODREADS_COOKIE")),
		GoodreadsMode:        get("GOODREADS_MODE"),
		HardcoverToken:       SecretString(get("HARDCOVER_TOKEN")),
		HardcoverUsername:    get("HARDCOVER_USERNAME"),
		Shelves:              splitCSV(get("SHELVES")),
		Format:               orDefault(get("FORMAT"), "ebook"),
		SimilarityThreshold:  0.82,
		FirstRun:             orDefault(get("FIRST_RUN"), "baseline"),
		MaxRequestsPerRun:    25,
		LangInference:        get("LANG_INFERENCE") != "off",
		ShelfarrInsecure:     get("SHELFARR_INSECURE") == "true",
		ShelfarrAutoRetry:    get("SHELFARR_AUTORETRY") == "true",
		ShelfarrAutoRetryMax: 3,
		Schedule:             orDefault(get("SCHEDULE"), "0 * * * *"),
		CWAEnabled:           get("CWA_ENABLED") == "true",
		CWAURL:               get("CWA_URL"),
		CWAUsername:          get("CWA_USERNAME"),
		CWAPassword:          SecretString(get("CWA_PASSWORD")),
		CWADateColumn:        get("CWA_DATE_COLUMN"),
		GUIPort:              orDefault(get("GUI_PORT"), "7373"),
		GUIBind:              orDefault(get("GUI_BIND"), "0.0.0.0"),
		AuthMethod:           orDefault(get("AUTH_METHOD"), "forms"),
		AuthRequired:         orDefault(get("AUTH_REQUIRED"), "local"),
	}
	if v := get("SIMILARITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.SimilarityThreshold = f
		}
	}
	if v := get("MAX_REQUESTS_PER_RUN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxRequestsPerRun = n
		}
	}
	if v := get("SHELFARR_AUTORETRY_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.ShelfarrAutoRetryMax = n
		}
	}
	// Shelfarr URL/token are NOT required at load time: the daemon + GUI must
	// start without them so they can be configured in the GUI. Use
	// ShelfarrConfigured() before running a sync.
	return c, nil
}

// ShelfarrConfigured reports whether the Shelfarr URL + token are set.
func (c Config) ShelfarrConfigured() bool {
	return c.ShelfarrURL != "" && c.ShelfarrToken.Reveal() != ""
}

// CWAConfigured reports whether CWA push is enabled and has URL + credentials.
func (c Config) CWAConfigured() bool {
	return c.CWAEnabled && c.CWAURL != "" && c.CWAUsername != "" && c.CWAPassword.Reveal() != ""
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// LoadEffective overlays stored settings (keyed by env-var name) over the
// environment, then parses as usual — so GUI-edited settings win over env.
func LoadEffective(getenv func(string) string, settings map[string]string) (Config, error) {
	merged := func(k string) string {
		if v, ok := settings[k]; ok && v != "" {
			return v
		}
		return getenv(k)
	}
	return loadFrom(merged)
}
