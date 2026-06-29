package shelfarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

type SearchResult struct {
	WorkID     string `json:"work_id"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Year       int    `json:"year"`
	Confidence *int   `json:"confidence"`
	HasEbook   *bool  `json:"has_ebook"`
	CoverURL   string `json:"cover_url"`
}

func (c *Client) Search(ctx context.Context, q string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	req, err := c.newReq("GET", "/api/v1/search?limit="+strconv.Itoa(limit)+"&q="+url.QueryEscape(q))
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("shelfarr search: HTTP %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("shelfarr search decode: %w", err)
	}
	return out.Results, nil
}
