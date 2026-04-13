package jitsi

import (
	"context"
	"fmt"
	"sync"

	"github.com/rtcbench/rtcbench"
	ivfpkg "github.com/rtcbench/rtcbench/pkg/ivf"
	jitsi "github.com/rtcbench/rtcbench/plugin/jitsi/internal"
)

const PluginID = "jitsi"

type Plugin struct {
	client  *jitsi.Client
	cameras *ivfpkg.Cameras
}

func NewPlugin() rtcbench.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(_ context.Context, e rtcbench.PluginEnv) error {
	client, err := jitsi.NewClient(e)
	if err != nil {
		return err
	}
	p.client = client
	p.client.SetRecording(
		e.Config().Spec.Conference.Recording.Enabled,
		e.Config().Spec.Conference.Recording.Directory,
	)
	cams, err := rtcbench.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("jitsi: %w", err)
	}
	p.cameras = cams

	return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	p.client.Shutdown() // TODO: pass ctx
	return nil
}

func (p *Plugin) NewParticipant(ctx context.Context, cfg *rtcbench.UserConfig) (rtcbench.Participant, error) {
	return &participant{
		plugin: p,
		userID: cfg.UserID,
		role:   cfg.Role,
	}, nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role rtcbench.UserRole, roomID, userID string) error {
	part, err := p.NewParticipant(ctx, &rtcbench.UserConfig{
		UserID: userID,
		Role:   role,
	})
	if err != nil {
		return err
	}
	return part.JoinRoom(ctx, &rtcbench.JoinRequest{RoomID: roomID, Plugin: PluginID})
}

type participant struct {
	mu      sync.Mutex
	plugin  *Plugin
	userID  string
	role    rtcbench.UserRole
	session *jitsi.Session
}

func (p *participant) JoinRoom(ctx context.Context, req *rtcbench.JoinRequest) error {
	if req == nil || req.RoomID == "" {
		return rtcbench.ErrMissingRoomID
	}
	p.mu.Lock()
	if p.session != nil {
		p.mu.Unlock()
		return rtcbench.ErrConnectionExists
	}
	p.mu.Unlock()

	if p.role != rtcbench.Viewer && p.role != rtcbench.Sender {
		return rtcbench.ErrUnsupportedRole
	}

	var src ivfpkg.FrameSource
	var cameraPaths []string
	if p.role == rtcbench.Sender && p.plugin.cameras != nil {
		src = p.plugin.cameras.NewSource()
		cameraPaths = p.plugin.cameras.Paths()
	}

	session, err := p.plugin.client.ConnectViewer(ctx, req.RoomID, p.userID, src, cameraPaths)
	if err != nil {
		return fmt.Errorf("%w: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	p.mu.Lock()
	p.session = session
	p.mu.Unlock()

	return nil
}

func (p *participant) LeaveRoom(ctx context.Context, req *rtcbench.LeaveRequest) error {
	return p.Close()
}

func (p *participant) PublishVideo(ctx context.Context, req *rtcbench.PublishVideoRequest) error {
	p.mu.Lock()
	session := p.session
	role := p.role
	p.mu.Unlock()

	if role != rtcbench.Sender {
		return rtcbench.ErrUnsupportedCapability
	}
	if session == nil {
		return fmt.Errorf("%w: participant not joined", rtcbench.ErrCannotJoinRoom)
	}
	return session.StartPublishing()
}

func (p *participant) UnpublishVideo(ctx context.Context, req *rtcbench.UnpublishVideoRequest) error {
	p.mu.Lock()
	session := p.session
	role := p.role
	p.mu.Unlock()

	if role != rtcbench.Sender {
		return rtcbench.ErrUnsupportedCapability
	}
	if session == nil {
		return nil
	}
	return session.StopPublishing()
}

func (p *participant) Close() error {
	p.mu.Lock()
	session := p.session
	p.session = nil
	p.mu.Unlock()
	if session != nil {
		return session.Close()
	}
	return nil
}
