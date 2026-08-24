// Package gatewayclient mirrors GatewayClientConfig's RestClient bean — a
// single outbound HTTP client to lumo-api-gateway, sending
// X-Internal-Api-Key on every request. A single outbound call site doesn't
// warrant anything heavier than net/http here either.
package gatewayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Client struct {
	http           *http.Client
	baseURL        string
	internalSecret string
}

func New(baseURL, internalSecret string) *Client {
	return &Client{
		http:           &http.Client{Timeout: 10 * time.Second},
		baseURL:        baseURL,
		internalSecret: internalSecret,
	}
}

// GetJSON issues a GET to path and, on a 200 response, decodes the JSON body
// into out. Returns the HTTP status code alongside any transport/decode error.
func (c *Client) GetJSON(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Internal-Api-Key", c.internalSecret)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

// PostJSON issues a POST with a JSON-encoded body to path, discarding the
// response body — every current caller only cares whether it succeeded.
func (c *Client) PostJSON(ctx context.Context, path string, body any) (int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Api-Key", c.internalSecret)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
