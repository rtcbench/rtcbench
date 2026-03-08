package internal

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/proto"
)

// SignalClient handles the LiveKit WebSocket signaling connection.
// It dispatches server messages to typed channels so callers can
// select on the exact messages they care about.
type SignalClient struct {
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex

	// Server → Client (all buffered; drop on full to avoid blocking the read loop)
	Answers     chan *livekit.SessionDescription      // answer for publisher PC
	Offers      chan *livekit.SessionDescription      // offer for subscriber PC (also renegotiation)
	TricklesPub chan *livekit.TrickleRequest          // ICE candidates for publisher PC
	TricklesSub chan *livekit.TrickleRequest          // ICE candidates for subscriber PC
	Published   chan *livekit.TrackPublishedResponse  // server ack after AddTrackRequest
}

// Connect dials the LiveKit signal endpoint, waits for the JoinResponse,
// and starts the background read/dispatch loop.
func Connect(
	ctx context.Context,
	serverURL string, // e.g. "wss://livekit.example.com"
	token string,
	allowInsecure bool,
	logf func(string, ...any),
) (*SignalClient, *livekit.JoinResponse, error) {
	ctx, cancel := context.WithCancel(ctx)

	wsURL := fmt.Sprintf(
		"%s/rtc?access_token=%s&auto_subscribe=1&protocol=9&sdk=go",
		serverURL, token,
	)

	httpClient := &http.Client{}
	if allowInsecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: httpClient,
	})
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("signal dial: %w", err)
	}

	// First message from server must be JoinResponse.
	_, data, err := conn.Read(ctx)
	if err != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		cancel()
		return nil, nil, fmt.Errorf("signal read join: %w", err)
	}
	var firstResp livekit.SignalResponse
	if err := proto.Unmarshal(data, &firstResp); err != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		cancel()
		return nil, nil, fmt.Errorf("signal unmarshal join: %w", err)
	}
	joinResp := firstResp.GetJoin()
	if joinResp == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		cancel()
		return nil, nil, fmt.Errorf("signal: first message is not JoinResponse (got %T)", firstResp.Message)
	}

	c := &SignalClient{
		ws:          conn,
		ctx:         ctx,
		cancel:      cancel,
		Answers:     make(chan *livekit.SessionDescription, 4),
		Offers:      make(chan *livekit.SessionDescription, 16),
		TricklesPub: make(chan *livekit.TrickleRequest, 64),
		TricklesSub: make(chan *livekit.TrickleRequest, 64),
		Published:   make(chan *livekit.TrackPublishedResponse, 4),
	}
	go c.readLoop(logf)
	return c, joinResp, nil
}

func (c *SignalClient) readLoop(logf func(string, ...any)) {
	defer c.cancel()
	for {
		_, data, err := c.ws.Read(c.ctx)
		if err != nil {
			if c.ctx.Err() == nil {
				logf("[signal] read error: %v", err)
			}
			return
		}

		var resp livekit.SignalResponse
		if err := proto.Unmarshal(data, &resp); err != nil {
			logf("[signal] unmarshal error: %v", err)
			continue
		}

		switch msg := resp.Message.(type) {
		case *livekit.SignalResponse_Answer:
			select {
			case c.Answers <- msg.Answer:
			default:
				logf("[signal] Answers channel full, dropping")
			}
		case *livekit.SignalResponse_Offer:
			select {
			case c.Offers <- msg.Offer:
			default:
				logf("[signal] Offers channel full, dropping")
			}
		case *livekit.SignalResponse_Trickle:
			t := msg.Trickle
			if t.Target == livekit.SignalTarget_PUBLISHER {
				select {
				case c.TricklesPub <- t:
				default:
				}
			} else {
				select {
				case c.TricklesSub <- t:
				default:
				}
			}
		case *livekit.SignalResponse_TrackPublished:
			select {
			case c.Published <- msg.TrackPublished:
			default:
			}
		case *livekit.SignalResponse_Leave:
			logf("[signal] server sent Leave")
			return
		default:
			// pong, speaker updates, etc. — ignore
		}
	}
}

// Send marshals and writes a SignalRequest over the WebSocket.
func (c *SignalClient) Send(req *livekit.SignalRequest) error {
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("signal marshal: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.Write(c.ctx, websocket.MessageBinary, data)
}

// Close shuts down the signal connection.
func (c *SignalClient) Close() {
	c.cancel()
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}
