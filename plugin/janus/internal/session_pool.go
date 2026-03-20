package internal

import (
	"context"
	"sync"

	"call.zip/pkg/log"
)

// SessionPool shares a single Janus session (long-poll + keepalive)
// across all viewers that join the same room. This reduces per-viewer
// HTTP overhead from O(N) sessions to O(rooms) sessions.
type SessionPool struct {
	client   *Client
	log      *log.Logger
	mu       sync.Mutex
	sessions map[int64]*Session // roomID → shared session
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewSessionPool(ctx context.Context, client *Client, log *log.Logger) *SessionPool {
	ctx, cancel := context.WithCancel(ctx)
	return &SessionPool{
		client:   client,
		log:      log,
		sessions: make(map[int64]*Session),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Get returns the shared session for roomID, creating one on first call.
func (sp *SessionPool) Get(roomID int64) (*Session, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if s, ok := sp.sessions[roomID]; ok {
		return s, nil
	}

	sessionID, err := sp.client.CreateSession()
	if err != nil {
		return nil, err
	}
	sp.log.Infof("shared session created for room %d: %d", roomID, sessionID)

	s := NewSession(sp.ctx, sp.client, sessionID, sp.log)
	sp.sessions[roomID] = s
	return s, nil
}

// Close cancels all pooled sessions.
func (sp *SessionPool) Close() {
	sp.cancel()
}
