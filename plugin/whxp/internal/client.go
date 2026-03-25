package internal

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client handles WHIP/WHEP HTTP signaling.
type Client struct {
	http       *http.Client
	bearerToken string
}

// NewClient creates a new WHIP/WHEP signaling client.
func NewClient(httpClient *http.Client, bearerToken string) *Client {
	return &Client{
		http:       httpClient,
		bearerToken: bearerToken,
	}
}

// WHIP sends an SDP offer to the WHIP endpoint and returns the SDP answer.
func (c *Client) WHIP(endpoint, offerSDP string) (answerSDP string, resourceURL string, err error) {
	return c.postSDP(endpoint, offerSDP)
}

// WHEP sends an SDP offer to the WHEP endpoint and returns the SDP answer.
func (c *Client) WHEP(endpoint, offerSDP string) (answerSDP string, resourceURL string, err error) {
	return c.postSDP(endpoint, offerSDP)
}

func (c *Client) postSDP(endpoint, offerSDP string) (string, string, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(offerSDP))
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sdp")
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("POST %s: HTTP %d: %s", endpoint, resp.StatusCode, string(body))
	}

	resourceURL := resp.Header.Get("Location")
	return string(body), resourceURL, nil
}

// Delete sends a DELETE request to the resource URL to tear down the session.
func (c *Client) Delete(resourceURL string) error {
	if resourceURL == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodDelete, resourceURL, nil)
	if err != nil {
		return err
	}
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
