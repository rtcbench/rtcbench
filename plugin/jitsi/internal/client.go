package jitsi

import (
	"fmt"

	"call.zip"
	ivfpkg "call.zip/pkg/ivf"
	"call.zip/pkg/log"
)

type Client struct {
	pipeline           *call.StatsPipeline
	log                *log.Logger
	serverIP           string
	clientIP           string
	statsBufferSize    int
	packetCaptureDir   string
	svcConfig          call.SVCCameraConfig
	enableRecording    bool
	recordingDirectory string
}

func NewClient(e call.PluginEnv) (*Client, error) {
	cfg := e.Config()

	pipeline := call.NewStatsPipeline(e)

	pcapDir, pcapErr := call.SetupPacketCaptureDir(cfg.Spec.Conference.PacketCapture)
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
