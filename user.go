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

type PublishVideoRequest struct {
	RoomID string
	Plugin string
}

type UnpublishVideoRequest struct {
	RoomID string
	Plugin string
}

type Participant interface {
	JoinRoom(ctx context.Context, req *JoinRequest) error
	LeaveRoom(ctx context.Context, req *LeaveRequest) error
	Close() error
}

type VideoPublisher interface {
	PublishVideo(ctx context.Context, req *PublishVideoRequest) error
	UnpublishVideo(ctx context.Context, req *UnpublishVideoRequest) error
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

	var staleConn *connection
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return ErrUserClosed
	}
	if existing, exists := u.connections[key]; exists {
		switch existing.state {
		case StateJoined, StateNew:
			u.mu.Unlock()
			return ErrConnectionExists
		case StateLeft:
			existing.state = StateClosed
			staleConn = existing
		case StateClosed:
			staleConn = existing
		}
	}
	u.mu.Unlock()

	if staleConn != nil {
		if err := staleConn.dp.Close(); err != nil {
			return fmt.Errorf("close stale %s/%s: %w", pluginID, req.RoomID, err)
		}
	}

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
	targets := u.selectConnections(targetSelector{
		plugin: selectorPlugin(req),
		roomID: selectorRoomID(req),
	})
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

func (u *User) PublishVideo(ctx context.Context, req *PublishVideoRequest) error {
	targets, err := u.joinedConnections(targetSelector{
		plugin: selectorPlugin(req),
		roomID: selectorRoomID(req),
	})
	if err != nil {
		return err
	}

	var (
		errs        []error
		supported   int
		successful  int
		unsupported int
	)
	for _, conn := range targets {
		vp, ok := conn.dp.(VideoPublisher)
		if !ok {
			continue
		}
		supported++
		pubReq := &PublishVideoRequest{Plugin: conn.plugin, RoomID: conn.roomID}
		if req != nil {
			if req.Plugin != "" {
				pubReq.Plugin = req.Plugin
			}
			if req.RoomID != "" {
				pubReq.RoomID = req.RoomID
			}
		}
		if err := vp.PublishVideo(ctx, pubReq); err != nil {
			if errors.Is(err, ErrUnsupportedCapability) {
				unsupported++
				continue
			}
			errs = append(errs, fmt.Errorf("publish video %s/%s: %w", conn.plugin, conn.roomID, err))
			continue
		}
		successful++
	}
	if supported == 0 && len(targets) > 0 {
		errs = append(errs, ErrUnsupportedCapability)
	}
	if supported > 0 && successful == 0 && unsupported > 0 && len(errs) == 0 {
		errs = append(errs, ErrUnsupportedCapability)
	}
	return errors.Join(errs...)
}

func (u *User) UnpublishVideo(ctx context.Context, req *UnpublishVideoRequest) error {
	targets, err := u.joinedConnections(targetSelector{
		plugin: selectorPlugin(req),
		roomID: selectorRoomID(req),
	})
	if err != nil {
		return err
	}

	var (
		errs        []error
		supported   int
		successful  int
		unsupported int
	)
	for _, conn := range targets {
		vp, ok := conn.dp.(VideoPublisher)
		if !ok {
			continue
		}
		supported++
		unpubReq := &UnpublishVideoRequest{Plugin: conn.plugin, RoomID: conn.roomID}
		if req != nil {
			if req.Plugin != "" {
				unpubReq.Plugin = req.Plugin
			}
			if req.RoomID != "" {
				unpubReq.RoomID = req.RoomID
			}
		}
		if err := vp.UnpublishVideo(ctx, unpubReq); err != nil {
			if errors.Is(err, ErrUnsupportedCapability) {
				unsupported++
				continue
			}
			errs = append(errs, fmt.Errorf("unpublish video %s/%s: %w", conn.plugin, conn.roomID, err))
			continue
		}
		successful++
	}
	if supported == 0 && len(targets) > 0 {
		errs = append(errs, ErrUnsupportedCapability)
	}
	if supported > 0 && successful == 0 && unsupported > 0 && len(errs) == 0 {
		errs = append(errs, ErrUnsupportedCapability)
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

type targetSelector struct {
	plugin string
	roomID string
}

type pluginRoomSelector interface {
	GetPlugin() string
	GetRoomID() string
}

func selectorPlugin(req interface{ GetPlugin() string }) string {
	if req == nil {
		return ""
	}
	return req.GetPlugin()
}

func selectorRoomID(req interface{ GetRoomID() string }) string {
	if req == nil {
		return ""
	}
	return req.GetRoomID()
}

func (r *LeaveRequest) GetPlugin() string {
	if r == nil {
		return ""
	}
	return r.Plugin
}
func (r *LeaveRequest) GetRoomID() string {
	if r == nil {
		return ""
	}
	return r.RoomID
}
func (r *PublishVideoRequest) GetPlugin() string {
	if r == nil {
		return ""
	}
	return r.Plugin
}
func (r *PublishVideoRequest) GetRoomID() string {
	if r == nil {
		return ""
	}
	return r.RoomID
}
func (r *UnpublishVideoRequest) GetPlugin() string {
	if r == nil {
		return ""
	}
	return r.Plugin
}
func (r *UnpublishVideoRequest) GetRoomID() string {
	if r == nil {
		return ""
	}
	return r.RoomID
}

func (u *User) selectConnections(sel targetSelector) []*connection {
	u.mu.RLock()
	defer u.mu.RUnlock()

	targets := make([]*connection, 0, len(u.connections))
	for key, conn := range u.connections {
		if sel.plugin != "" && sel.plugin != key.plugin {
			continue
		}
		if sel.roomID != "" && sel.roomID != key.roomID {
			continue
		}
		targets = append(targets, conn)
	}
	return targets
}

func (u *User) joinedConnections(sel targetSelector) ([]*connection, error) {
	u.mu.RLock()
	defer u.mu.RUnlock()

	if u.closed {
		return nil, ErrUserClosed
	}

	targets := make([]*connection, 0, len(u.connections))
	for key, conn := range u.connections {
		if sel.plugin != "" && sel.plugin != key.plugin {
			continue
		}
		if sel.roomID != "" && sel.roomID != key.roomID {
			continue
		}
		if conn.state == StateJoined {
			targets = append(targets, conn)
		}
	}
	if len(targets) == 0 {
		return nil, ErrConnectionNotJoined
	}
	return targets, nil
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
