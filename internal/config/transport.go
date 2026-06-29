package config

import (
	"fmt"
	"net"
	"net/url"
)

// CheckTransport refuses to send the bearer token in cleartext over a non-loopback
// http:// endpoint unless explicitly allowed.
func CheckTransport(shelfarrURL string, allowInsecure bool) error {
	u, err := url.Parse(shelfarrURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid SHELFARR_URL %q", shelfarrURL)
	}
	if u.Scheme == "https" || allowInsecure {
		return nil
	}
	if u.Scheme != "http" {
		return fmt.Errorf("SHELFARR_URL must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to send the Shelfarr token in cleartext to non-loopback http host %q; use https or set SHELFARR_INSECURE=true", host)
}
