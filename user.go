package rtcbench

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type UserConfig struct {
	UserID string
	Role   UserRole
}

type JoinRequest struct {
	RoomID string
	Plugin string
}

type LeaveRequest struct {
	RoomID string
	Plugin string
}

type Participant interface {
	JoinRoom(ctx context.Context, req *JoinRequest) error
	LeaveRoom(ctx context.Context, req *LeaveRequest) error
	Close() error
}

type ConnectionState int

const (
	StateNew ConnectionState = iota
	StateJoined
	StateLeft
	StateClosed
)

type User struct {
	mu            sync.RWMutex
	client        *Client
	id            string
	role          UserRole
	defaultPlugin string
	connections   map[connectionKey]*connection
	log           interface {
		Infof(string, ...any)
		Errorf(string, ...any)
	}
	closed bool
}

type connectionKey struct {
	plugin string
	roomID string
}

type connection struct {
	plugin string
	roomID string
	dp     Participant
	state  ConnectionState
}

func (u *User) JoinRoom(ctx context.Context, req *JoinRequest) error {
	if req == nil || req.RoomID == "" {
		return ErrMissingRoomID
	}

	pluginID := req.Plugin
	if pluginID == "" {
		pluginID = u.defaultPlugin
	}
	if pluginID == "" {
		return ErrUnknownPlugin
	}

	key := connectionKey{plugin: pluginID, roomID: req.RoomID}

	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return ErrUserClosed
	}
	if _, exists := u.connections[key]; exists {
		u.mu.Unlock()
		return ErrConnectionExists
	}
	u.mu.Unlock()

	if err := u.client.SetupPlugin(ctx, pluginID); err != nil {
		return err
	}

	plugin := u.client.getPlugin(pluginID)
	if plugin == nil {
		return ErrUnknownPlugin
	}

	dp, err := newParticipant(ctx, plugin, &UserConfig{
		UserID: u.id,
		Role:   u.role,
	})
	if err != nil {
		return err
	}

	joinReq := *req
	joinReq.Plugin = pluginID
	if err := dp.JoinRoom(ctx, &joinReq); err != nil {
		_ = dp.Close()
		return err
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		_ = dp.LeaveRoom(ctx, &LeaveRequest{Plugin: pluginID, RoomID: req.RoomID})
		_ = dp.Close()
		return ErrUserClosed
	}
	u.connections[key] = &connection{
		plugin: pluginID,
		roomID: req.RoomID,
		dp:     dp,
		state:  StateJoined,
	}
	return nil
}

func (u *User) LeaveRoom(ctx context.Context, req *LeaveRequest) error {
	targets := u.selectConnections(req)
	var errs []error
	for _, conn := range targets {
		if conn.state != StateJoined {
			continue
		}
		leaveReq := &LeaveRequest{Plugin: conn.plugin, RoomID: conn.roomID}
		if req != nil {
			leaveReq = &LeaveRequest{Plugin: req.Plugin, RoomID: req.RoomID}
			if leaveReq.Plugin == "" {
				leaveReq.Plugin = conn.plugin
			}
			if leaveReq.RoomID == "" {
				leaveReq.RoomID = conn.roomID
			}
		}
		if err := conn.dp.LeaveRoom(ctx, leaveReq); err != nil {
			errs = append(errs, fmt.Errorf("leave %s/%s: %w", conn.plugin, conn.roomID, err))
			continue
		}
		u.mu.Lock()
		conn.state = StateLeft
		u.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (u *User) Close() error {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return nil
	}
	u.closed = true
	targets := make([]*connection, 0, len(u.connections))
	for _, conn := range u.connections {
		targets = append(targets, conn)
	}
	u.mu.Unlock()

	var errs []error
	for _, conn := range targets {
		if conn.state == StateJoined {
			if err := conn.dp.LeaveRoom(context.Background(), &LeaveRequest{
				Plugin: conn.plugin,
				RoomID: conn.roomID,
			}); err != nil {
				errs = append(errs, fmt.Errorf("leave %s/%s: %w", conn.plugin, conn.roomID, err))
			}
		}
		if conn.state != StateClosed {
			if err := conn.dp.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close %s/%s: %w", conn.plugin, conn.roomID, err))
			}
			u.mu.Lock()
			conn.state = StateClosed
			u.mu.Unlock()
		}
	}

	if u.client != nil {
		u.client.removeUser(u.id)
	}

	return errors.Join(errs...)
}

func (u *User) selectConnections(req *LeaveRequest) []*connection {
	u.mu.RLock()
	defer u.mu.RUnlock()

	targets := make([]*connection, 0, len(u.connections))
	for key, conn := range u.connections {
		if req != nil && req.Plugin != "" && req.Plugin != key.plugin {
			continue
		}
		if req != nil && req.RoomID != "" && req.RoomID != key.roomID {
			continue
		}
		targets = append(targets, conn)
	}
	return targets
}

func newParticipant(ctx context.Context, plugin Plugin, cfg *UserConfig) (Participant, error) {
	if pp, ok := plugin.(ParticipantPlugin); ok {
		return pp.NewParticipant(ctx, cfg)
	}
	return &legacyParticipant{
		plugin: plugin,
		userID: cfg.UserID,
		role:   cfg.Role,
	}, nil
}

type legacyParticipant struct {
	plugin Plugin
	userID string
	role   UserRole

	mu     sync.Mutex
	joined bool
	left   bool
}

func (p *legacyParticipant) JoinRoom(ctx context.Context, req *JoinRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.joined {
		return ErrConnectionExists
	}
	if err := p.plugin.JoinRoom(ctx, p.role, req.RoomID, p.userID); err != nil {
		return err
	}
	p.joined = true
	return nil
}

func (p *legacyParticipant) LeaveRoom(ctx context.Context, req *LeaveRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.joined || p.left {
		return nil
	}
	p.left = true
	return nil
}

func (p *legacyParticipant) Close() error {
	return nil
}
