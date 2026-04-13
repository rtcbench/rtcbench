package jitsi

import (
	"context"
	"fmt"

	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench"
	ivfpkg "github.com/rtcbench/rtcbench/pkg/ivf"
	"github.com/rtcbench/rtcbench/pkg/log"
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
}

type Session struct {
	pc     *webrtc.PeerConnection
	cancel context.CancelFunc
}

func (s *Session) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.pc != nil {
		return s.pc.Close()
	}
	return nil
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
	}, nil
}

func (c *Client) SetRecording(enabled bool, directory string) {
	c.enableRecording = enabled
	c.recordingDirectory = directory
}

func (c *Client) ConnectViewer(ctx context.Context, roomID, userID string, src ivfpkg.FrameSource, cameraPaths []string) (*Session, error) {
	l := c.log.With(fmt.Sprintf("[%s]", userID))
	return c.performHandshake(ctx, l, roomID, userID, src, cameraPaths)
}

func (c *Client) Shutdown() {
	c.pipeline.Stop()
}
