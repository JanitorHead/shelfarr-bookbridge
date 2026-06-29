package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ShelfarrURL         string
	ShelfarrToken       SecretString
	GoodreadsUserID     string
	GoodreadsFeedKey    SecretString
	GoodreadsCookie     SecretString
	Shelves             []string
	Format              string
	SimilarityThreshold float64
	FirstRun            string
	MaxRequestsPerRun   int
}

func Load() (Config, error) { return loadFrom(os.Getenv) }

// Load2 loads config from a custom getenv (used by the CLI/tests).
func Load2(get func(string) string) (Config, error) { return loadFrom(get) }

func loadFrom(get func(string) string) (Config, error) {
	c := Config{
		ShelfarrURL:         get("SHELFARR_URL"),
		ShelfarrToken:       SecretString(get("SHELFARR_TOKEN")),
		GoodreadsUserID:     get("GOODREADS_USER_ID"),
		GoodreadsFeedKey:    SecretString(get("GOODREADS_FEED_KEY")),
		GoodreadsCookie:     SecretString(get("GOODREADS_COOKIE")),
		Shelves:             splitCSV(get("SHELVES")),
		Format:              orDefault(get("FORMAT"), "ebook"),
		SimilarityThreshold: 0.82,
		FirstRun:            orDefault(get("FIRST_RUN"), "baseline"),
		MaxRequestsPerRun:   25,
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
	if c.ShelfarrURL == "" || c.ShelfarrToken.Reveal() == "" {
		return c, fmt.Errorf("SHELFARR_URL and SHELFARR_TOKEN are required")
	}
	return c, nil
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
