package internal

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(httpClient *http.Client, baseURL string) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

func (c *Client) GetInfo() (map[string]any, error) {
	info, err := c.getJSON("/info")

	if err != nil {
		return nil, err
	}

	if info["janus"].(string) != "server_info" {
		return nil, errors.New("request to /info json does not appear to be server_info")
	}

	return info, nil
}

func (c *Client) getJSON(path string) (map[string]any, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out, nil
}
