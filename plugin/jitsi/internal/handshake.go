package jitsi

import (
	"context"
	"fmt"
	"net/url"
	"time"

	ivfpkg "call.zip/pkg/ivf"
	"call.zip/pkg/log"
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
	l *log.Logger,
	room string,
	nickname string,
	src ivfpkg.FrameSource,
	cameraPaths []string,
) error {
	state := &model.ConnectionState{
		Sender:      src != nil,
		FrameSource: src,
		CameraPaths: cameraPaths,
		Log:         l,
		BOSHSender:  httpxml.NewBOSHSender(l),
		BoshURL:     fmt.Sprintf("https://%s/http-bind", c.serverIP),
		RoomName:    room,
		MachineUID:  uuid.NewString(),
		RID:         initialRid(),
		Nickname:    nickname,
		LANServerIP: c.serverIP,
		LANClientIP: c.clientIP,
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
			l.Errorf("step failed: %v", err)
			return err
		}
	}

	if err := steps.Step09_WaitForJingleOffer(state); err != nil {
		l.Errorf("Step09 failed: %v", err)
		return err
	}

	sdp, err := sdp_tmpl.RenderSDP(sdp_tmpl.SDPState{
		ICEUfrag:    state.ICEUfrag,
		ICEPwd:      state.ICEPwd,
		Fingerprint: state.Fingerprint,
		Sender:      state.Sender,
	})
	if err != nil {
		l.Errorf("SDP conversion failed: %v", err)
		return err
	}
	state.RemoteSDP = sdp

	pionConnection, err := c.startPion(state)
	if err != nil {
		l.Errorf("startPion failed: %v", err)
		return err
	}

	// pionConnection generates local SDP (answer) with real ICE creds.
	state.LocalSDP = pionConnection.LocalDescription().SDP

	if state.Sender {
		if err := steps.Step10_Sender_SendSessionAccept(state); err != nil {
			l.Errorf("Step10_Sender failed: %v", err)
			return err
		}
		if err := steps.Step11_Sender_AnnounceCameraSource(state); err != nil {
			l.Errorf("Step11_Sender failed: %v", err)
			return err
		}
	} else {
		if err := steps.Step10_SendSessionAccept(state); err != nil {
			l.Errorf("Step10 failed: %v", err)
			return err
		}
	}

	if state.ColibriWebSocketURL != "" {
		go runColibriWS(context.Background(), state.ColibriWebSocketURL, l)
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
				l.Errorf("[keepalive] %v", err)
				return
			}
			l.Debugf("[keepalive] sent ping")
		}

		sendPing() // fire immediately — don't wait 5s for the first one
		for {
			<-ticker.C
			sendPing()
		}
	}()

	return nil
}
