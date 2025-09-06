package client

import (
	"call.zip/internal/aofconf"
	"call.zip/internal/bot"
)

type Client struct {
	botManager *bot.Manager
	logMode    string
	serverIP   string
	clientIP   string
}

func NewClient(botManager *bot.Manager, logMode, serverIP, clientIP string) *Client {
	return &Client{
		botManager: botManager,
		logMode:    logMode,
		serverIP:   serverIP,
		clientIP:   clientIP,
	}
}

func (c *Client) ConnectViewer(room, nickname string, replay bool) error {
	aof := buildAOFConfig(c.logMode, c.serverIP, c.clientIP, room, nickname)
	return c.performHandshake(aof, room, nickname, replay)
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
