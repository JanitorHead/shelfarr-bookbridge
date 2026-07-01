package shelfarr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type CreateRequestParams struct {
	WorkID    string   `json:"work_id"`
	BookTypes []string `json:"book_types"`
	Language  string   `json:"language,omitempty"`
	Title     string   `json:"title,omitempty"`
	Author    string   `json:"author,omitempty"`
	CoverURL  string   `json:"cover_url,omitempty"`
	Year      int      `json:"year,omitempty"`
}

// CreateRequest POSTs a request. A duplicate (HTTP 422 whose error mentions an
// existing request / already in library) is reported as alreadyExists=true with
// no error — the caller resolves the existing request separately.
func (c *Client) CreateRequest(ctx context.Context, p CreateRequestParams) (string, bool, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return "", false, err
	}
	req, err := c.newReq("POST", "/api/v1/requests")
	if err != nil {
		return "", false, err
	}
	req.Body = io.NopCloser(bytes.NewReader(payload))
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Requests []struct {
			ID json.RawMessage `json:"id"`
		} `json:"requests"`
		Errors []string `json:"errors"`
	}
	_ = json.Unmarshal(body, &parsed)

	if resp.StatusCode == 422 {
		joined := strings.ToLower(strings.Join(parsed.Errors, " "))
		if strings.Contains(joined, "already") {
			return "", true, nil
		}
		return "", false, fmt.Errorf("shelfarr create 422: %s", body)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", false, fmt.Errorf("shelfarr create: HTTP %d: %s", resp.StatusCode, body)
	}
	if len(parsed.Requests) == 0 {
		return "", false, fmt.Errorf("shelfarr create: no request in response: %s", body)
	}
	id := strings.Trim(string(parsed.Requests[0].ID), `"`)
	return id, false, nil
}

// Retry asks Shelfarr to retry a request (re-grab the selected release, or
// restart the search). Note: Shelfarr does NOT blocklist the failed release, so
// this un-sticks transient failures but does not force a different method.
func (c *Client) Retry(ctx context.Context, id string) error {
	req, err := c.newReq("POST", "/api/v1/requests/"+id+"/retry")
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return ErrRequestNotFound
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("shelfarr retry %s: HTTP %d: %s", id, resp.StatusCode, body)
	}
	return nil
}
