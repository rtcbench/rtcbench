package jitsi

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/coder/websocket"
)

const receiverVideoConstraints = `{"colibriClass":"ReceiverVideoConstraints","lastN":-1,"selectedSources":[],"onStageSources":[],"defaultConstraints":{"maxHeight":1080}}`

func runColibriWS(ctx context.Context, wsURL string, l *log.Logger) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: httpClient,
	})
	if err != nil {
		l.Errorf("[colibri-ws] dial failed: %v", err)
		return
	}
	defer conn.CloseNow()

	l.Infof("[colibri-ws] connected to %s", wsURL)

	if err := conn.Write(ctx, websocket.MessageText, []byte(receiverVideoConstraints)); err != nil {
		l.Errorf("[colibri-ws] send ReceiverVideoConstraints failed: %v", err)
		return
	}
	l.Debugf("[colibri-ws] sent ReceiverVideoConstraints")

	// Read messages from JVB to keep the connection alive.
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			l.Debugf("[colibri-ws] read error (connection closed): %v", err)
			return
		}
		l.Debugf("[colibri-ws] received: %s", msg)
	}
}
