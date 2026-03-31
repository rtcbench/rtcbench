package jitsi

import (
	"fmt"

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

func (c *Client) ConnectViewer(roomID, userID string, src ivfpkg.FrameSource, cameraPaths []string) error {
	l := c.log.With(fmt.Sprintf("[%s]", userID))
	return c.performHandshake(l, roomID, userID, src, cameraPaths)
}

func (c *Client) Shutdown() {
	c.pipeline.Stop()
}
