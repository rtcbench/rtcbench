package client

import (
	"log"

	"call.zip/internal/aofconf"
	"call.zip/internal/bot"
	"call.zip/internal/vp9_stats"
)

type Client struct {
	botManager *bot.Manager
	publisher  *vp9_stats.Publisher
	logMode    string
	serverIP   string
	clientIP   string
}

type ViewerConfig struct {
	RoomName string
	Nickname string
	Replay   bool
}

func NewClient(logMode, serverIP, clientIP string, inputChanSize int64) *Client {
	input := make(chan vp9_stats.VideoQualitySample, inputChanSize)
	botManager := bot.NewManager(input)
	publisher := vp9_stats.NewPublisher(input)

	publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		log.Printf("[sub-data-v1] period=%s,sample=%s", period.String(), sample.String())
	})

	go publisher.Run()

	return &Client{
		botManager: botManager,
		publisher:  publisher,
		logMode:    logMode,
		serverIP:   serverIP,
		clientIP:   clientIP,
	}
}

func (c *Client) ConnectViewer(vc ViewerConfig) error {
	aof := buildAOFConfig(c.logMode, c.serverIP, c.clientIP, vc.RoomName, vc.Nickname)
	return c.performHandshake(aof, vc.RoomName, vc.Nickname, vc.Replay)
}

func (c *Client) Shutdown() {
	c.botManager.StopAll()
	c.publisher.Stop()
}

func buildAOFConfig(logMode, serverIP, clientIP, room, nickname string) *aofconf.Config {
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
