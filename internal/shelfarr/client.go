package shelfarr

import (
	"net/http"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

type Client struct {
	base  string
	token config.SecretString
	hc    *http.Client
}

func New(baseURL string, token config.SecretString, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: baseURL, token: token, hc: hc}
}

func (c *Client) newReq(method, path string) (*http.Request, error) {
	req, err := http.NewRequest(method, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	req.Header.Set("Accept", "application/json")
	return req, nil
}
