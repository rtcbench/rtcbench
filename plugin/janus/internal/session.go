package internal

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session manages one Janus HTTP session: runs an event-poll loop and
// dispatches events to pending transactions.
type Session struct {
	client    *Client
	sessionID int64
	mu        sync.Mutex
	pending   map[string]chan *JanusMsg
	eventsC   chan *JanusMsg
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewSession(ctx context.Context, client *Client, sessionID int64) *Session {
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{
		client:    client,
		sessionID: sessionID,
		pending:   make(map[string]chan *JanusMsg),
		eventsC:   make(chan *JanusMsg, 64),
		ctx:       ctx,
		cancel:    cancel,
	}
	go s.loop()
	return s
}

// Send sends a message to the given handle and waits (up to 30s) for the
// corresponding event to arrive via long-poll.
func (s *Session) Send(handleID int64, body map[string]any, jsep *JSEP) (*JanusMsg, error) {
	txn := uuid.NewString()

	ch := make(chan *JanusMsg, 1)
	s.mu.Lock()
	s.pending[txn] = ch
	s.mu.Unlock()

	resp, err := s.client.SendMessage(s.sessionID, handleID, body, jsep, txn)
	if err != nil {
		s.mu.Lock()
		delete(s.pending, txn)
		s.mu.Unlock()
		return nil, err
	}

	// Janus can respond synchronously (not an "ack") for some operations.
	// If so, use that response directly rather than waiting for long-poll.
	if resp != nil && resp.Janus != "ack" {
		s.mu.Lock()
		delete(s.pending, txn)
		s.mu.Unlock()
		return resp, nil
	}

	select {
	case msg := <-ch:
		return msg, nil
	case <-time.After(30 * time.Second):
		s.mu.Lock()
		delete(s.pending, txn)
		s.mu.Unlock()
		return nil, fmt.Errorf("janus: timeout waiting for transaction %s", txn)
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// Events returns a channel of unsolicited events (not matched to a pending txn).
func (s *Session) Events() <-chan *JanusMsg {
	return s.eventsC
}

func (s *Session) Close() {
	s.cancel()
}

func (s *Session) loop() {
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				if err := s.client.Keepalive(s.sessionID); err != nil {
					log.Printf("[janus] keepalive error: %v", err)
				}
			}
		}
	}()

	for {
		if s.ctx.Err() != nil {
			return
		}
		msgs, err := s.client.LongPoll(s.ctx, s.sessionID)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			log.Printf("[janus] long poll error: %v", err)
			time.Sleep(time.Second)
			continue
		}
		for _, msg := range msgs {
			s.dispatch(msg)
		}
	}
}

func (s *Session) dispatch(msg *JanusMsg) {
	// Acks are consumed synchronously by SendMessage; ignore them here.
	if msg.Janus == "ack" || msg.Janus == "keepalive" {
		return
	}

	if msg.Transaction != "" {
		s.mu.Lock()
		ch, ok := s.pending[msg.Transaction]
		if ok {
			delete(s.pending, msg.Transaction)
		}
		s.mu.Unlock()

		if ok {
			select {
			case ch <- msg:
			default:
			}
			return
		}
	}

	select {
	case s.eventsC <- msg:
	default:
		log.Printf("[janus] event channel full, dropping: %s", msg.Janus)
	}
}
