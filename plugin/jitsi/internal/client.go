package jitsi

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"call.zip"
	ivfpkg "call.zip/pkg/ivf"
	"call.zip/pkg/log"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
)

type Client struct {
	botManager       *viewer.Manager
	publisher        *vp9_stats.Publisher
	log              *log.Logger
	serverIP         string
	clientIP         string
	statsBufferSize  int
	packetCaptureDir string
}

func NewClient(cfg *call.Config, inputChanSize int64) *Client {
	input := make(chan vp9_stats.VideoQualitySample, inputChanSize)

	statsLog := cfg.Log.NewLogger("video_stats", "")
	botManager := viewer.NewManager(input, statsLog)
	publisher := vp9_stats.NewPublisher(input)

	publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		statsLog.Infof("bitrate=%s,period=%s,sample=%s", sample.Mbps(), period.String(), sample.String())
	})
	for _, consumer := range cfg.StatsConsumers {
		publisher.AddSubscriber(consumer)
	}

	go publisher.Run()

	var pcapDir string
	if cfg.Spec.Conference.PacketCapture.Enabled {
		ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
		pcapDir = filepath.Join(cfg.Spec.Conference.PacketCapture.Directory, ts)
		_ = os.MkdirAll(pcapDir, 0o755)
	}

	return &Client{
		botManager:       botManager,
		publisher:        publisher,
		log:              cfg.Log.NewLogger("jitsi", ""),
		serverIP:         cfg.Spec.Network.ServerIP,
		clientIP:         cfg.Spec.Network.ClientIP,
		statsBufferSize:  cfg.Spec.Conference.StatsBufferSize,
		packetCaptureDir: pcapDir,
	}
}

func (c *Client) ConnectViewer(roomID, userID string, ivf *vp9.IvfSegmenter, src ivfpkg.FrameSource) error {
	l := c.log.With(fmt.Sprintf("[%s]", userID))
	return c.performHandshake(l, roomID, userID, ivf, src)
}

func (c *Client) Shutdown() {
	c.botManager.StopAll()
	c.publisher.Stop()
}
