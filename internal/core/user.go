package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
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
	metrics UserMetrics
	closed  bool
}

type UserMetrics struct {
	JoinAttempts               int64
	JoinSuccesses              int64
	JoinFailures               int64
	JoinLatencyTotal           time.Duration
	LeaveAttempts              int64
	LeaveSuccesses             int64
	LeaveFailures              int64
	LeaveLatencyTotal          time.Duration
	PublishVideoAttempts       int64
	PublishVideoSuccesses      int64
	PublishVideoFailures       int64
	PublishVideoLatencyTotal   time.Duration
	UnpublishVideoAttempts     int64
	UnpublishVideoSuccesses    int64
	UnpublishVideoFailures     int64
	UnpublishVideoLatencyTotal time.Duration
	CloseAttempts              int64
	CloseSuccesses             int64
	CloseFailures              int64
	CloseLatencyTotal          time.Duration
}

type connectionKey struct {
	plugin string
	roomID string
}

type connection struct {
	plugin         string
	roomID         string
	dp             Participant
	state          ConnectionState
	joinedAt       time.Time
	videoPublished bool
}

func (u *User) JoinRoom(ctx context.Context, req *JoinRequest) error {
	startedAt := time.Now()
	pluginID := u.defaultPlugin
	if req != nil && req.Plugin != "" {
		pluginID = req.Plugin
	}
	labels := u.lifecycleLabels(normalizePluginLabel(pluginID))
	u.recordAttempt("join")
	u.recordRunAttempt("join", labels)
	if req == nil || req.RoomID == "" {
		err := ErrMissingRoomID
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", "", "", err)
		return err
	}

	if pluginID == "" {
		err := ErrUnknownPlugin
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", "", req.RoomID, err)
		return err
	}

	u.log.Infof("[user] join start plugin=%s room=%s", pluginID, req.RoomID)

	key := connectionKey{plugin: pluginID, roomID: req.RoomID}

	var staleConn *connection
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		err := ErrUserClosed
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
		return err
	}
	if existing, exists := u.connections[key]; exists {
		switch existing.state {
		case StateJoined, StateNew:
			u.mu.Unlock()
			err := ErrConnectionExists
			u.recordResult("join", startedAt, err)
			u.recordRunResult("join", labels, startedAt, err)
			u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
			return err
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
			err = fmt.Errorf("close stale %s/%s: %w", pluginID, req.RoomID, err)
			u.recordResult("join", startedAt, err)
			u.recordRunResult("join", labels, startedAt, err)
			u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
			return err
		}
	}

	if err := u.client.SetupPlugin(ctx, pluginID); err != nil {
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
		return err
	}

	plugin := u.client.getPlugin(pluginID)
	if plugin == nil {
		err := ErrUnknownPlugin
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
		return err
	}

	dp, err := newParticipant(ctx, plugin, &UserConfig{
		UserID: u.id,
		Role:   u.role,
	})
	if err != nil {
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
		return err
	}

	joinReq := *req
	joinReq.Plugin = pluginID
	if err := dp.JoinRoom(ctx, &joinReq); err != nil {
		_ = dp.Close()
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
		return err
	}

	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		_ = dp.LeaveRoom(ctx, &LeaveRequest{Plugin: pluginID, RoomID: req.RoomID})
		_ = dp.Close()
		err := ErrUserClosed
		u.recordResult("join", startedAt, err)
		u.recordRunResult("join", labels, startedAt, err)
		u.log.Errorf("[user] join failed plugin=%s room=%s err=%v", pluginID, req.RoomID, err)
		return err
	}
	u.connections[key] = &connection{
		plugin:   pluginID,
		roomID:   req.RoomID,
		dp:       dp,
		state:    StateJoined,
		joinedAt: time.Now(),
	}
	u.mu.Unlock()
	u.recordResult("join", startedAt, nil)
	u.recordRunResult("join", labels, startedAt, nil)
	u.recordConnectionJoined(labels, req.RoomID)
	u.log.Infof("[user] join ok plugin=%s room=%s latency=%s", pluginID, req.RoomID, time.Since(startedAt))
	return nil
}

func (u *User) LeaveRoom(ctx context.Context, req *LeaveRequest) error {
	startedAt := time.Now()
	opLabels := u.lifecycleLabels(u.operationPluginLabel(selectorPlugin(req)))
	u.recordAttempt("leave")
	u.recordRunAttempt("leave", opLabels)
	targets := u.selectConnections(targetSelector{
		plugin: selectorPlugin(req),
		roomID: selectorRoomID(req),
	})
	var errs []error
	targeted := 0
	for _, conn := range targets {
		if conn.state != StateJoined {
			continue
		}
		targeted++
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
		wasPublished := false
		joinedAt := time.Time{}
		u.mu.Lock()
		wasPublished = conn.videoPublished
		joinedAt = conn.joinedAt
		conn.videoPublished = false
		conn.state = StateLeft
		u.mu.Unlock()
		u.recordConnectionLeft(u.lifecycleLabels(conn.plugin), conn.roomID, wasPublished)
		u.recordSessionDuration(u.lifecycleLabels(conn.plugin), joinedAt, time.Now())
		u.log.Infof("[user] leave ok plugin=%s room=%s", conn.plugin, conn.roomID)
	}
	err := errors.Join(errs...)
	u.recordResult("leave", startedAt, err)
	u.recordRunResult("leave", opLabels, startedAt, err)
	if err != nil {
		u.log.Errorf("[user] leave failed plugin=%s room=%s err=%v", selectorPlugin(req), selectorRoomID(req), err)
	} else if targeted == 0 {
		u.log.Infof("[user] leave noop plugin=%s room=%s", selectorPlugin(req), selectorRoomID(req))
	}
	return err
}

func (u *User) PublishVideo(ctx context.Context, req *PublishVideoRequest) error {
	startedAt := time.Now()
	opLabels := u.lifecycleLabels(u.operationPluginLabel(selectorPlugin(req)))
	u.recordAttempt("publish_video")
	u.recordRunAttempt("publish_video", opLabels)
	targets, err := u.joinedConnections(targetSelector{
		plugin: selectorPlugin(req),
		roomID: selectorRoomID(req),
	})
	if err != nil {
		u.recordResult("publish_video", startedAt, err)
		u.recordRunResult("publish_video", opLabels, startedAt, err)
		u.log.Errorf("[user] publish-video failed plugin=%s room=%s err=%v", selectorPlugin(req), selectorRoomID(req), err)
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
		shouldRecordPublish := false
		u.mu.Lock()
		if !conn.videoPublished {
			conn.videoPublished = true
			shouldRecordPublish = true
		}
		u.mu.Unlock()
		if shouldRecordPublish {
			u.recordVideoPublished(u.lifecycleLabels(conn.plugin))
		}
		successful++
		u.log.Infof("[user] publish-video ok plugin=%s room=%s", conn.plugin, conn.roomID)
	}
	if supported == 0 && len(targets) > 0 {
		errs = append(errs, ErrUnsupportedCapability)
	}
	if supported > 0 && successful == 0 && unsupported > 0 && len(errs) == 0 {
		errs = append(errs, ErrUnsupportedCapability)
	}
	err = errors.Join(errs...)
	u.recordResult("publish_video", startedAt, err)
	u.recordRunResult("publish_video", opLabels, startedAt, err)
	if err != nil {
		u.log.Errorf("[user] publish-video failed plugin=%s room=%s err=%v", selectorPlugin(req), selectorRoomID(req), err)
	}
	return err
}

func (u *User) UnpublishVideo(ctx context.Context, req *UnpublishVideoRequest) error {
	startedAt := time.Now()
	opLabels := u.lifecycleLabels(u.operationPluginLabel(selectorPlugin(req)))
	u.recordAttempt("unpublish_video")
	u.recordRunAttempt("unpublish_video", opLabels)
	targets, err := u.joinedConnections(targetSelector{
		plugin: selectorPlugin(req),
		roomID: selectorRoomID(req),
	})
	if err != nil {
		u.recordResult("unpublish_video", startedAt, err)
		u.recordRunResult("unpublish_video", opLabels, startedAt, err)
		u.log.Errorf("[user] unpublish-video failed plugin=%s room=%s err=%v", selectorPlugin(req), selectorRoomID(req), err)
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
		shouldRecordUnpublish := false
		u.mu.Lock()
		if conn.videoPublished {
			conn.videoPublished = false
			shouldRecordUnpublish = true
		}
		u.mu.Unlock()
		if shouldRecordUnpublish {
			u.recordVideoUnpublished(u.lifecycleLabels(conn.plugin))
		}
		successful++
		u.log.Infof("[user] unpublish-video ok plugin=%s room=%s", conn.plugin, conn.roomID)
	}
	if supported == 0 && len(targets) > 0 {
		errs = append(errs, ErrUnsupportedCapability)
	}
	if supported > 0 && successful == 0 && unsupported > 0 && len(errs) == 0 {
		errs = append(errs, ErrUnsupportedCapability)
	}
	err = errors.Join(errs...)
	u.recordResult("unpublish_video", startedAt, err)
	u.recordRunResult("unpublish_video", opLabels, startedAt, err)
	if err != nil {
		u.log.Errorf("[user] unpublish-video failed plugin=%s room=%s err=%v", selectorPlugin(req), selectorRoomID(req), err)
	}
	return err
}

func (u *User) Close() error {
	return u.closeWithContext(context.Background())
}

func (u *User) closeWithContext(ctx context.Context) error {
	startedAt := time.Now()
	opLabels := u.lifecycleLabels(u.closeOperationPluginLabel())
	u.recordAttempt("close")
	u.recordRunAttempt("close", opLabels)
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		u.recordResult("close", startedAt, nil)
		u.recordRunResult("close", opLabels, startedAt, nil)
		u.log.Infof("[user] close noop")
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
		u.mu.RLock()
		wasJoined := conn.state == StateJoined
		wasPublished := conn.videoPublished
		joinedAt := conn.joinedAt
		u.mu.RUnlock()
		if conn.state == StateJoined {
			if err := conn.dp.LeaveRoom(ctx, &LeaveRequest{
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
			conn.videoPublished = false
			conn.state = StateClosed
			u.mu.Unlock()
		}
		if wasJoined {
			u.recordConnectionLeft(u.lifecycleLabels(conn.plugin), conn.roomID, wasPublished)
			u.recordSessionDuration(u.lifecycleLabels(conn.plugin), joinedAt, time.Now())
		}
	}

	if u.client != nil {
		u.client.removeUser(u.id)
	}

	err := errors.Join(errs...)
	u.recordResult("close", startedAt, err)
	u.recordRunResult("close", opLabels, startedAt, err)
	if err != nil {
		u.log.Errorf("[user] close failed err=%v", err)
	} else {
		u.log.Infof("[user] close ok latency=%s", time.Since(startedAt))
	}
	return err
}

func (u *User) MetricsSnapshot() UserMetrics {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.metrics
}

func (u *User) recordAttempt(op string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch op {
	case "join":
		u.metrics.JoinAttempts++
	case "leave":
		u.metrics.LeaveAttempts++
	case "publish_video":
		u.metrics.PublishVideoAttempts++
	case "unpublish_video":
		u.metrics.UnpublishVideoAttempts++
	case "close":
		u.metrics.CloseAttempts++
	}
}

func (u *User) recordRunAttempt(op string, labels Labels) {
	if u.client == nil || u.client.metrics == nil {
		return
	}
	u.client.metrics.RecordOperationAttempt(labels, op)
}

func (u *User) recordResult(op string, startedAt time.Time, err error) {
	elapsed := time.Since(startedAt)
	u.mu.Lock()
	defer u.mu.Unlock()
	switch op {
	case "join":
		u.metrics.JoinLatencyTotal += elapsed
		if err == nil {
			u.metrics.JoinSuccesses++
		} else {
			u.metrics.JoinFailures++
		}
	case "leave":
		u.metrics.LeaveLatencyTotal += elapsed
		if err == nil {
			u.metrics.LeaveSuccesses++
		} else {
			u.metrics.LeaveFailures++
		}
	case "publish_video":
		u.metrics.PublishVideoLatencyTotal += elapsed
		if err == nil {
			u.metrics.PublishVideoSuccesses++
		} else {
			u.metrics.PublishVideoFailures++
		}
	case "unpublish_video":
		u.metrics.UnpublishVideoLatencyTotal += elapsed
		if err == nil {
			u.metrics.UnpublishVideoSuccesses++
		} else {
			u.metrics.UnpublishVideoFailures++
		}
	case "close":
		u.metrics.CloseLatencyTotal += elapsed
		if err == nil {
			u.metrics.CloseSuccesses++
		} else {
			u.metrics.CloseFailures++
		}
	}
}

func (u *User) recordRunResult(op string, labels Labels, startedAt time.Time, err error) {
	if u.client == nil || u.client.metrics == nil {
		return
	}
	u.client.metrics.RecordOperationResult(labels, op, time.Since(startedAt).Seconds(), err)
}

func (u *User) recordConnectionJoined(labels Labels, roomID string) {
	if u.client == nil || u.client.metrics == nil {
		return
	}
	u.client.metrics.ConnectionJoined(u.id, roomID, labels)
}

func (u *User) recordConnectionLeft(labels Labels, roomID string, wasPublished bool) {
	if u.client == nil || u.client.metrics == nil {
		return
	}
	u.client.metrics.ConnectionLeft(u.id, roomID, labels, wasPublished)
}

func (u *User) recordVideoPublished(labels Labels) {
	if u.client == nil || u.client.metrics == nil {
		return
	}
	u.client.metrics.VideoPublished(labels)
}

func (u *User) recordVideoUnpublished(labels Labels) {
	if u.client == nil || u.client.metrics == nil {
		return
	}
	u.client.metrics.VideoUnpublished(labels)
}

func (u *User) recordSessionDuration(labels Labels, startedAt, endedAt time.Time) {
	if u.client == nil || u.client.metrics == nil {
		return
	}
	if startedAt.IsZero() || endedAt.Before(startedAt) {
		return
	}
	u.client.metrics.ObserveSessionDuration(labels, endedAt.Sub(startedAt).Seconds())
}

func (u *User) lifecycleLabels(pluginID string) Labels {
	return Labels{
		metricLabelScenario: u.scenarioLabel(),
		metricLabelPlugin:   normalizePluginLabel(pluginID),
		metricLabelRole:     normalizeRoleLabel(u.role),
	}
}

func (u *User) scenarioLabel() string {
	if u.client == nil {
		return manualScenarioLabel
	}
	return u.client.currentScenarioLabel()
}

func (u *User) operationPluginLabel(pluginID string) string {
	if pluginID != "" {
		return pluginID
	}
	return metricLabelAll
}

func (u *User) closeOperationPluginLabel() string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	plugins := make(map[string]struct{})
	for _, conn := range u.connections {
		plugins[conn.plugin] = struct{}{}
	}
	if len(plugins) == 1 {
		for pluginID := range plugins {
			return pluginID
		}
	}
	return metricLabelAll
}

func normalizePluginLabel(pluginID string) string {
	if pluginID == "" {
		return metricLabelUnknown
	}
	return pluginID
}

func normalizeRoleLabel(role UserRole) string {
	if role == "" {
		return metricLabelUnknown
	}
	return string(role)
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
	return plugin.NewParticipant(ctx, cfg)
}
