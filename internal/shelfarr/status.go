package shelfarr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrRequestNotFound = errors.New("shelfarr request not found (404)")

// RequestStatus is the subset of a Shelfarr request we act on. We deliberately
// do NOT model the numeric "id" here (Shelfarr returns it as a JSON number, and
// we already know the id we queried) — unknown JSON fields are ignored.
type RequestStatus struct {
	Status           string `json:"status"`
	IssueDescription string `json:"issue_description"`
	AttentionNeeded  bool   `json:"attention_needed"`
}

func (c *Client) GetRequest(ctx context.Context, id string) (RequestStatus, error) {
	var rs RequestStatus
	req, err := c.newReq("GET", "/api/v1/requests/"+id)
	if err != nil {
		return rs, err
	}
	resp, err := c.hc.Do(req.WithContext(ctx))
	if err != nil {
		return rs, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return rs, ErrRequestNotFound
	}
	if resp.StatusCode != 200 {
		return rs, fmt.Errorf("shelfarr get request %s: HTTP %d: %s", id, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &rs); err != nil {
		return rs, err
	}
	return rs, nil
}
