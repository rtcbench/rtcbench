package jitsi

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"call.zip/pkg/aofconf"
	ivfpkg "call.zip/pkg/ivf"
	"call.zip/pkg/vp9"
	"call.zip/plugin/jitsi/internal/httpxml"
	"call.zip/plugin/jitsi/internal/model"
	"call.zip/plugin/jitsi/internal/sdp_tmpl"
	"call.zip/plugin/jitsi/internal/steps"
	"github.com/google/uuid"
)

func initialRid() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func (c *Client) performHandshake(
	aof *aofconf.Config,
	room string,
	nickname string,
	ivf *vp9.IvfSegmenter,
	src ivfpkg.FrameSource,
) error {
	state := &model.ConnectionState{
		Sender:      src != nil,
		FrameSource: src,
		StepsAOF:       aof.Steps,
		PionAOF:        aof.Pion,
		PktRecvAOF:     aof.PktRecv,
		NetStatsAOF:    aof.NetStats,
		BOSHSender:     httpxml.NewBOSHSender(aof.BOSH),
		BoshURL:        fmt.Sprintf("https://%s/http-bind", c.serverIP),
		RoomName:       room,
		MachineUID:     uuid.NewString(),
		RID:            initialRid(),
		Nickname:       nickname,
		LANServerIP:    c.serverIP,
		LANClientIP:    c.clientIP,
		LogsDir:        aof.LogsDir,
	}

	u, _ := url.Parse(state.BoshURL)
	state.Domain = u.Hostname()

	stepsBeforeJingle := []func(*model.ConnectionState) error{
		steps.Step01_StartSession,
		steps.Step02_Authenticate,
		steps.Step03_RestartStream,
		steps.Step04_BindResource,
		steps.Step05_CreateSession,
		steps.Step06_DiscoverServices,
		steps.Step07_CreateConference,
		steps.Step08_JoinRoom,
	}

	for _, step := range stepsBeforeJingle {
		err := step(state)
		if err != nil {
			aof.Handshake.LogPrintf("Step failed: %v", err)
			return err
		}
	}

	if err := steps.Step09_WaitForJingleOffer(state); err != nil {
		aof.Handshake.LogPrintf("Step09 failed: %v", err)
		return err
	}

	sdp, err := sdp_tmpl.RenderSDP(sdp_tmpl.SDPState{
		ICEUfrag:    state.ICEUfrag,
		ICEPwd:      state.ICEPwd,
		Fingerprint: state.Fingerprint,
		Sender:      state.Sender,
	})
	if err != nil {
		aof.Handshake.LogPrintf("SDP conversion failed: %v", err)
		return err
	}
	state.RemoteSDP = sdp

	pionConnection, err := c.startPion(state, ivf)
	if err != nil {
		aof.Handshake.LogPrintf("startPion failed: %v", err)
		return err
	}

	// pionConnection generates local SDP (answer) with real ICE creds.
	state.LocalSDP = pionConnection.LocalDescription().SDP

	if state.Sender {
		if err := steps.Step10_Sender_SendSessionAccept(state); err != nil {
			aof.Handshake.LogPrintf("Step10_Sender failed: %v", err)
			return err
		}
		if err := steps.Step11_Sender_AnnounceCameraSource(state); err != nil {
			aof.Handshake.LogPrintf("Step11_Sender failed: %v", err)
			return err
		}
	} else {
		if err := steps.Step10_SendSessionAccept(state); err != nil {
			aof.Handshake.LogPrintf("Step10 failed: %v", err)
			return err
		}
	}

	if state.ColibriWebSocketURL != "" {
		go runColibriWS(context.Background(), state.ColibriWebSocketURL, aof.KeepAlive)
	}

	// BOSH keepalive: send XMPP pings to keep the Prosody session alive.
	// Without this, Prosody expires the session and Jicofo tells JVB to
	// stop forwarding this sender's media.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		sendPing := func() {
			state.RID++
			requestBody := fmt.Sprintf(`<body rid="%d" sid="%s" xmlns="http://jabber.org/protocol/httpbind">
<iq id="%s:sendIQ" to="%s" type="get" xmlns="jabber:client">
<ping xmlns="urn:xmpp:ping"/></iq></body>`,
				state.RID,
				state.Sid,
				uuid.NewString(),
				c.serverIP,
			)

			_, err := state.BOSHSender.Send(state.BoshURL, requestBody)
			if err != nil {
				aof.KeepAlive.LogPrintf("[KeepAlive] ERROR: %v", err)
				return
			}
			aof.KeepAlive.LogPrintln("[KeepAlive] Sent keepalive ping.")
		}

		sendPing() // fire immediately — don't wait 5s for the first one
		for {
			<-ticker.C
			sendPing()
		}
	}()

	return nil
}
