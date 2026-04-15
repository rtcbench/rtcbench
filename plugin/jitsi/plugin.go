package jitsi

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rtcbench/rtcbench"
	ivfpkg "github.com/rtcbench/rtcbench/pkg/ivf"
	"github.com/rtcbench/rtcbench/pkg/pionutil"
	jitsi "github.com/rtcbench/rtcbench/plugin/jitsi/internal"
)

const PluginID = "jitsi"

type Plugin struct {
	client             *jitsi.Client
	cameras            *ivfpkg.Cameras
	impairmentRouter   *pionutil.ImpairmentRouter
	observeReceiverRTT func(string, time.Duration)
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
	p.impairmentRouter = e.ImpairmentRouter()
	p.client.SetRecording(
		e.Config().Spec.Conference.Recording.Enabled,
		e.Config().Spec.Conference.Recording.Directory,
	)
	cams, err := rtcbench.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("jitsi: %w", err)
	}
	p.cameras = cams
	p.observeReceiverRTT = e.ObserveReceiverRTT

	return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	p.client.Shutdown() // TODO: pass ctx
	return nil
}

func (p *Plugin) NewParticipant(ctx context.Context, cfg *rtcbench.UserConfig) (rtcbench.Participant, error) {
	var impairment *pionutil.ImpairmentBinding
	if cfg.ImpairmentProfile != nil && p.impairmentRouter != nil {
		impairment = &pionutil.ImpairmentBinding{
			Router:  p.impairmentRouter,
			Profile: pionProfileFromCore(cfg.ImpairmentProfile),
		}
	}
	return &participant{
		plugin:     p,
		userID:     cfg.UserID,
		role:       cfg.Role,
		impairment: impairment,
	}, nil
}

func pionProfileFromCore(c *rtcbench.ImpairmentProfile) *pionutil.ImpairmentProfile {
	if c == nil {
		return nil
	}
	return &pionutil.ImpairmentProfile{
		Name:         c.Name,
		BandwidthBps: c.BandwidthBps,
		BaseLatency:  c.BaseLatency,
		JitterStddev: c.JitterStddev,
		LossPercent:  c.LossPercent,
		Seed:         c.Seed,
	}
}

type participant struct {
	mu         sync.Mutex
	plugin     *Plugin
	userID     string
	role       rtcbench.UserRole
	impairment *pionutil.ImpairmentBinding
	session    *jitsi.Session
	rttCancel  context.CancelFunc
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

	session, err := p.plugin.client.ConnectViewer(ctx, req.RoomID, p.userID, src, cameraPaths, p.impairment)
	if err != nil {
		return fmt.Errorf("%w: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	var rttCancel context.CancelFunc
	if p.role == rtcbench.Viewer && p.plugin.observeReceiverRTT != nil {
		rttCtx, cancel := context.WithCancel(context.Background())
		rttCancel = cancel
		go pionutil.PollPeerConnectionRTT(
			rttCtx,
			2*time.Second,
			session.PeerConnection,
			func(rtt time.Duration) { p.plugin.observeReceiverRTT(p.userID, rtt) },
		)
	}

	p.mu.Lock()
	p.session = session
	p.rttCancel = rttCancel
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
	rttCancel := p.rttCancel
	p.session = nil
	p.rttCancel = nil
	p.mu.Unlock()
	if rttCancel != nil {
		rttCancel()
	}
	if session != nil {
		return session.Close()
	}
	return nil
}
