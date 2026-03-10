package jitsi

import (
	"log"

	"call.zip"
	"call.zip/pkg/aofconf"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
)

type Client struct {
	botManager *viewer.Manager
	publisher  *vp9_stats.Publisher
	serverIP   string
	clientIP   string
}

func NewClient(cfg *call.Config, inputChanSize int64) *Client {
	input := make(chan vp9_stats.VideoQualitySample, inputChanSize)
	botManager := viewer.NewManager(input)
	publisher := vp9_stats.NewPublisher(input)

	publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		// TODO: use logging framework not log.Printf
		log.Printf("[sub-data-v1] bitrate=%s,period=%s,sample=%s", sample.Mbps(), period.String(), sample.String())
	})
	for _, consumer := range cfg.StatsConsumers {
		publisher.AddSubscriber(consumer)
	}

	go publisher.Run()

	return &Client{
		botManager: botManager,
		publisher:  publisher,
		serverIP:   cfg.Spec.Network.ServerIP,
		clientIP:   cfg.Spec.Network.ClientIP,
	}
}

func (c *Client) ConnectViewer(roomID, userID string, ivf *vp9.IvfSegmenter, ivfPathsToStream []string) error {
	aof := buildAOFConfig(c.serverIP, c.clientIP, roomID, userID)
	return c.performHandshake(aof, roomID, userID, ivf, ivfPathsToStream)
}

func (c *Client) Shutdown() {
	c.botManager.StopAll()
	c.publisher.Stop()
}

func buildAOFConfig(serverIP, clientIP, roomID, userID string) *aofconf.Config {
	// TODO: logging has to be refactored to use the config
	logMode := "debug"
	room := roomID
	nickname := userID

	silentLogs := logMode == "silent"
	debugLogs := logMode == "split" || logMode == "debug" || logMode == "dbg"
	dumpPackets := logMode == "vb+"
	perVBotLogs := logMode == "vbot" || logMode == "bot" || logMode == "vb+"

	if silentLogs {
		return aofconf.NewNilClientConfig()
	} else if debugLogs {
		// packet dumps and logs written in multiple files in a folder per vbot
		return aofconf.NewDebugLogsClientConfig(serverIP, clientIP, room, nickname)
	} else if perVBotLogs {
		// logs written to single file per vbot
		return aofconf.NewPerVBotLogsClientConfig(serverIP, clientIP, room, nickname, dumpPackets)
	} else {
		// all logs go to stdout/stderr
		return aofconf.NewStdClientConfig(serverIP, clientIP, room, nickname)
	}
}
