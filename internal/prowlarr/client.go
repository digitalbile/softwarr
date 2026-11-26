// Package prowlarr is a small client for Prowlarr's search API.
package prowlarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Client struct {
	base   string
	apiKey string
	http   *http.Client
}

func New(baseURL, apiKey string, httpc *http.Client) *Client {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	return &Client{base: strings.TrimSpace(baseURL), apiKey: apiKey, http: httpc}
}

// Search queries Prowlarr. Empty categories or indexerIDs leave that filter off.
func (c *Client) Search(ctx context.Context, query string, categories, indexerIDs []int) ([]Release, error) {
	q := url.Values{}
	q.Set("query", query)
	q.Set("type", "search")
	for _, id := range categories {
		q.Add("categories", strconv.Itoa(id))
	}
	for _, id := range indexerIDs {
		q.Add("indexerIds", strconv.Itoa(id))
	}
	var wire []wireRelease
	if err := c.get(ctx, "/api/v1/search", q, &wire); err != nil {
		return nil, err
	}
	out := make([]Release, len(wire))
	for i, w := range wire {
		out[i] = w.release()
	}
	return out, nil
}

func (c *Client) Indexers(ctx context.Context) ([]Indexer, error) {
	var wire []struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Protocol     string `json:"protocol"`
		Enable       bool   `json:"enable"`
		Capabilities struct {
			Categories []wireCategory `json:"categories"`
		} `json:"capabilities"`
	}
	if err := c.get(ctx, "/api/v1/indexer", nil, &wire); err != nil {
		return nil, err
	}
	out := make([]Indexer, len(wire))
	for i, w := range wire {
		out[i] = Indexer{
			ID: w.ID, Name: w.Name, Protocol: w.Protocol, Enabled: w.Enable,
			Categories: flattenCategories(nil, w.Capabilities.Categories),
		}
	}
	return out, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.get(ctx, "/api/v1/system/status", nil, nil)
}

func (c *Client) get(ctx context.Context, path string, query url.Values, dst any) error {
	u, err := url.Parse(c.base)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid Prowlarr URL %q", c.base)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("Prowlarr returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(excerpt)))
	}
	if dst == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("read Prowlarr response: %w", err)
	}
	return nil
}
