package jitsi

import (
	"context"
	"fmt"
	"sync"

	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench"
	ivfpkg "github.com/rtcbench/rtcbench/pkg/ivf"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/pionutil"
)

type Client struct {
	pipeline           *rtcbench.StatsPipeline
	log                *log.Logger
	serverIP           string
	clientIP           string
	statsBufferSize    int
	packetCaptureDir   string
	svcConfig          rtcbench.SVCCameraConfig
	enableRecording    bool
	recordingDirectory string
	impairmentRouter   *pionutil.ImpairmentRouter
}

type Session struct {
	mu               sync.Mutex
	pc               *webrtc.PeerConnection
	cancel           context.CancelFunc
	startPublishLoop func() context.CancelFunc
	publishCancel    context.CancelFunc
	impairment       *pionutil.ImpairmentBinding
}

func (s *Session) Close() error {
	s.mu.Lock()
	publishCancel := s.publishCancel
	s.publishCancel = nil
	cancel := s.cancel
	pc := s.pc
	s.cancel = nil
	s.pc = nil
	s.mu.Unlock()

	if publishCancel != nil {
		publishCancel()
	}
	if cancel != nil {
		cancel()
	}
	if pc != nil {
		return pc.Close()
	}
	return nil
}

func (s *Session) StartPublishing() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startPublishLoop == nil {
		return rtcbench.ErrUnsupportedCapability
	}
	if s.publishCancel != nil {
		return nil
	}
	s.publishCancel = s.startPublishLoop()
	return nil
}

func (s *Session) StopPublishing() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startPublishLoop == nil {
		return rtcbench.ErrUnsupportedCapability
	}
	if s.publishCancel == nil {
		return nil
	}
	s.publishCancel()
	s.publishCancel = nil
	return nil
}

func (s *Session) IsSender() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startPublishLoop != nil
}

func (s *Session) PeerConnection() *webrtc.PeerConnection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pc
}

func (s *Session) ContextCancel() context.CancelFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel
}

func (s *Session) resetPublishing() {
	if s.cancel != nil {
		s.cancel()
	}
}

func NewClient(e rtcbench.PluginEnv) (*Client, error) {
	cfg := e.Config()

	pipeline := rtcbench.NewStatsPipeline(e)

	pcapDir, pcapErr := rtcbench.SetupPacketCaptureDir(cfg.Spec.Conference.PacketCapture)
	if pcapErr != nil {
		return nil, fmt.Errorf("jitsi: %w", pcapErr)
	}

	return &Client{
		pipeline:         pipeline,
		log:              e.LogRegistry().NewLogger("jitsi", ""),
		serverIP:         cfg.Spec.Network.ServerIP,
		clientIP:         cfg.Spec.Network.ClientIP,
		statsBufferSize:  cfg.Spec.Conference.StatsBufferSize,
		packetCaptureDir: pcapDir,
		svcConfig:        cfg.Spec.Conference.Cameras.SVC,
		impairmentRouter: e.ImpairmentRouter(),
	}, nil
}

func (c *Client) SetRecording(enabled bool, directory string) {
	c.enableRecording = enabled
	c.recordingDirectory = directory
}

func (c *Client) ConnectViewer(ctx context.Context, roomID, userID string, src ivfpkg.FrameSource, cameraPaths []string, impairment *pionutil.ImpairmentBinding) (*Session, error) {
	l := c.log.With(fmt.Sprintf("[%s]", userID))
	return c.performHandshake(ctx, l, roomID, userID, src, cameraPaths, impairment)
}

func (c *Client) Shutdown() {
	c.pipeline.Stop()
}

func (c *Client) ServerIP() string {
	return c.serverIP
}
