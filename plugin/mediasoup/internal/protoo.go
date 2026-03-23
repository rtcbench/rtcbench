package internal

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"call.zip/pkg/log"
	"github.com/coder/websocket"
)

// protooMsg is the union of all protoo message types.
type protooMsg struct {
	Request      bool            `json:"request,omitempty"`
	Response     bool            `json:"response,omitempty"`
	Notification bool            `json:"notification,omitempty"`
	ID           uint32          `json:"id,omitempty"`
	Method       string          `json:"method,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
	OK           *bool           `json:"ok,omitempty"`
	ErrorCode    int             `json:"errorCode,omitempty"`
	ErrorReason  string          `json:"errorReason,omitempty"`
}

type protooResponse struct {
	ok          bool
	data        json.RawMessage
	errorCode   int
	errorReason string
}

// RequestHandler handles server-initiated requests. Returns response data or error.
type RequestHandler func(method string, data json.RawMessage) (any, error)

// NotificationHandler handles server-initiated notifications.
type NotificationHandler func(method string, data json.RawMessage)

// Protoo is a protoo-protocol WebSocket client.
type Protoo struct {
	conn *websocket.Conn
	log  *log.Logger

	nextID  atomic.Uint32
	mu      sync.Mutex
	pending map[uint32]chan protooResponse

	onRequest      RequestHandler
	onNotification NotificationHandler

	ctx    context.Context
	cancel context.CancelFunc
}

// NewProtoo connects to the given protoo WebSocket URL and starts the read loop.
func NewProtoo(
	ctx context.Context,
	wsURL string,
	l *log.Logger,
	onReq RequestHandler,
	onNotify NotificationHandler,
) (*Protoo, error) {
	// mediasoup-demo validates the Origin header against its configured domain.
	// Derive the origin from the WebSocket URL so the check passes.
	parsed, parseErr := url.Parse(wsURL)
	if parseErr != nil {
		return nil, fmt.Errorf("protoo parse %s: %w", wsURL, parseErr)
	}
	scheme := "https"
	if parsed.Scheme == "ws" {
		scheme = "http"
	}
	origin := fmt.Sprintf("%s://%s", scheme, parsed.Host)

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"protoo"},
		HTTPHeader:   http.Header{"Origin": {origin}},
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("protoo dial %s: %w", wsURL, err)
	}
	conn.SetReadLimit(1 << 20) // 1 MB

	pctx, cancel := context.WithCancel(ctx)
	p := &Protoo{
		conn:           conn,
		log:            l,
		pending:        make(map[uint32]chan protooResponse),
		onRequest:      onReq,
		onNotification: onNotify,
		ctx:            pctx,
		cancel:         cancel,
	}
	go p.readLoop()
	return p, nil
}

// Close gracefully shuts down the connection.
func (p *Protoo) Close() {
	p.cancel()
	p.conn.Close(websocket.StatusNormalClosure, "")
}

// Request sends a protoo request and waits for the response.
func (p *Protoo) Request(method string, data any) (json.RawMessage, error) {
	id := p.nextID.Add(1)

	ch := make(chan protooResponse, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}()

	if data == nil {
		data = struct{}{}
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal request data: %w", err)
	}

	if err := p.send(protooMsg{
		Request: true,
		ID:      id,
		Method:  method,
		Data:    dataBytes,
	}); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		if !resp.ok {
			return nil, fmt.Errorf("protoo %s error %d: %s", method, resp.errorCode, resp.errorReason)
		}
		return resp.data, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("protoo %s request timeout", method)
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	}
}

// Notify sends a protoo notification (fire-and-forget).
func (p *Protoo) Notify(method string, data any) error {
	if data == nil {
		data = struct{}{}
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	return p.send(protooMsg{
		Notification: true,
		Method:       method,
		Data:         dataBytes,
	})
}

func (p *Protoo) send(msg protooMsg) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal protoo message: %w", err)
	}
	return p.conn.Write(p.ctx, websocket.MessageText, raw)
}

func (p *Protoo) readLoop() {
	defer p.cancel()
	for {
		_, raw, err := p.conn.Read(p.ctx)
		if err != nil {
			if p.ctx.Err() != nil {
				return
			}
			p.log.Errorf("[protoo] read error: %v", err)
			return
		}

		var msg protooMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			p.log.Errorf("[protoo] unmarshal error: %v", err)
			continue
		}

		switch {
		case msg.Response:
			p.handleResponse(msg)
		case msg.Request:
			go p.handleServerRequest(msg)
		case msg.Notification:
			if p.onNotification != nil {
				go p.onNotification(msg.Method, msg.Data)
			}
		}
	}
}

func (p *Protoo) handleResponse(msg protooMsg) {
	p.mu.Lock()
	ch, ok := p.pending[msg.ID]
	p.mu.Unlock()
	if !ok {
		return
	}
	isOK := msg.OK != nil && *msg.OK
	ch <- protooResponse{
		ok:          isOK,
		data:        msg.Data,
		errorCode:   msg.ErrorCode,
		errorReason: msg.ErrorReason,
	}
}

func (p *Protoo) handleServerRequest(msg protooMsg) {
	if p.onRequest == nil {
		p.respondToServer(msg.ID, false, nil, 500, "no handler")
		return
	}
	result, err := p.onRequest(msg.Method, msg.Data)
	if err != nil {
		p.respondToServer(msg.ID, false, nil, 500, err.Error())
		return
	}
	p.respondToServer(msg.ID, true, result, 0, "")
}

func (p *Protoo) respondToServer(id uint32, ok bool, data any, errCode int, errReason string) {
	var dataBytes json.RawMessage
	if data != nil {
		var err error
		dataBytes, err = json.Marshal(data)
		if err != nil {
			p.log.Errorf("[protoo] marshal response: %v", err)
			return
		}
	}
	resp := protooMsg{
		Response:    true,
		ID:          id,
		OK:          &ok,
		Data:        dataBytes,
		ErrorCode:   errCode,
		ErrorReason: errReason,
	}
	if err := p.send(resp); err != nil {
		p.log.Errorf("[protoo] send response: %v", err)
	}
}
