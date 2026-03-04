package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
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

type JSEP struct {
	Type    string `json:"type"`
	SDP     string `json:"sdp"`
	Trickle bool   `json:"trickle,omitempty"`
}

type JanusMsg struct {
	Janus       string         `json:"janus"`
	Transaction string         `json:"transaction"`
	PluginData  *PluginData    `json:"plugindata,omitempty"`
	JSEP        *JSEP          `json:"jsep,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	Error       *JanusError    `json:"error,omitempty"`
	Sender      int64          `json:"sender,omitempty"`
	SessionID   int64          `json:"session_id,omitempty"`
}

type PluginData struct {
	Plugin string         `json:"plugin"`
	Data   map[string]any `json:"data"`
}

type JanusError struct {
	Code   int    `json:"code"`
	Reason string `json:"reason"`
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

func (c *Client) postJSON(path string, body any) (*JanusMsg, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var msg JanusMsg
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, err
	}
	if msg.Error != nil {
		return nil, fmt.Errorf("janus error %d: %s", msg.Error.Code, msg.Error.Reason)
	}
	return &msg, nil
}

func (c *Client) CreateSession() (int64, error) {
	msg, err := c.postJSON("", map[string]any{
		"janus":       "create",
		"transaction": uuid.NewString(),
	})
	if err != nil {
		return 0, err
	}
	id, ok := msg.Data["id"]
	if !ok {
		return 0, errors.New("janus create: missing id in response")
	}
	idFloat, ok := id.(float64)
	if !ok {
		return 0, fmt.Errorf("janus create: unexpected id type %T", id)
	}
	return int64(idFloat), nil
}

func (c *Client) AttachPlugin(sessionID int64, plugin string) (int64, error) {
	msg, err := c.postJSON(fmt.Sprintf("/%d", sessionID), map[string]any{
		"janus":       "attach",
		"plugin":      plugin,
		"transaction": uuid.NewString(),
	})
	if err != nil {
		return 0, err
	}
	id, ok := msg.Data["id"]
	if !ok {
		return 0, errors.New("janus attach: missing id in response")
	}
	idFloat, ok := id.(float64)
	if !ok {
		return 0, fmt.Errorf("janus attach: unexpected id type %T", id)
	}
	return int64(idFloat), nil
}

func (c *Client) SendMessage(sessionID, handleID int64, body map[string]any, jsep *JSEP, txn string) (*JanusMsg, error) {
	req := map[string]any{
		"janus":       "message",
		"transaction": txn,
		"body":        body,
	}
	if jsep != nil {
		req["jsep"] = jsep
	}
	return c.postJSON(fmt.Sprintf("/%d/%d", sessionID, handleID), req)
}

func (c *Client) Keepalive(sessionID int64) error {
	_, err := c.postJSON(fmt.Sprintf("/%d", sessionID), map[string]any{
		"janus":       "keepalive",
		"transaction": uuid.NewString(),
	})
	return err
}

// LongPoll GETs /{sessionID}?maxev=5 and returns all events.
// With maxev>1 Janus always responds with a JSON array.
func (c *Client) LongPoll(ctx context.Context, sessionID int64) ([]*JanusMsg, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/%d?maxev=5", c.baseURL, sessionID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var msgs []*JanusMsg
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}
