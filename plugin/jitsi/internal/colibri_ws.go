package jitsi

import (
	"context"
	"crypto/tls"
	"net/http"

	"call.zip/pkg/aoflog"
	"github.com/coder/websocket"
)

const receiverVideoConstraints = `{"colibriClass":"ReceiverVideoConstraints","lastN":-1,"selectedSources":[],"onStageSources":[],"defaultConstraints":{"maxHeight":180}}`

func runColibriWS(ctx context.Context, wsURL string, aof aoflog.Client) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: httpClient,
	})
	if err != nil {
		aof.LogPrintf("[colibri-ws] Dial failed: %v", err)
		return
	}
	defer conn.CloseNow()

	aof.LogPrintf("[colibri-ws] Connected to %s", wsURL)

	if err := conn.Write(ctx, websocket.MessageText, []byte(receiverVideoConstraints)); err != nil {
		aof.LogPrintf("[colibri-ws] Send ReceiverVideoConstraints failed: %v", err)
		return
	}
	aof.LogPrintln("[colibri-ws] Sent ReceiverVideoConstraints")

	// Read messages from JVB to keep the connection alive.
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			aof.LogPrintf("[colibri-ws] Read error (connection closed): %v", err)
			return
		}
		aof.LogPrintf("[colibri-ws] Received: %s", msg)
	}
}
