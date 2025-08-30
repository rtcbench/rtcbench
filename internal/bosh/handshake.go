package bosh

import (
	"fmt"
	"net/url"
	"time"

	"call.zip/internal/aofconf"
	"call.zip/internal/bosh/httpxml"
	"call.zip/internal/bosh/model"
	"call.zip/internal/bosh/steps"
	"github.com/google/uuid"
)

func initialRid() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func PerformHandshake(aof *aofconf.Config, serverIP, clientIP, confName, viewerName string, enableClientLoopV2, enableAES128GCM bool) {
	state := &model.ConnectionState{
		StepsAOF:           aof.Steps,
		PionAOF:            aof.Pion,
		PktRecvAOF:         aof.PktRecv,
		NetStatsAOF:        aof.NetStats,
		BOSHSender:         httpxml.NewBOSHSender(aof.BOSH),
		BoshURL:            fmt.Sprintf("https://%s/http-bind", serverIP),
		RoomName:           confName,
		MachineUID:         uuid.NewString(),
		RID:                initialRid(),
		Nickname:           viewerName,
		LANServerIP:        serverIP,
		LANClientIP:        clientIP,
		LogsDir:            aof.LogsDir,
		DumpPackets:        aof.DumpPackets,
		EnableClientLoopV2: enableClientLoopV2,
		EnableAES128GCM:    enableAES128GCM,
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
			return
		}
	}

	if err := steps.Step09_WaitForJingleOffer(state); err != nil {
		aof.Handshake.LogPrintf("Step09 failed: %v", err)
		return
	}

	sdp, err := ConvertJingleToSDP(state)
	if err != nil {
		aof.Handshake.LogPrintf("SDP conversion failed: %v", err)
		return
	}
	state.RemoteSDP = sdp

	pionConnection, err := startPion(state)
	if err != nil {
		aof.Handshake.LogPrintf("startPion failed: %v", err)
		return
	}

	// pionConnection generates local SDP (answer) with real ICE creds.
	state.LocalSDP = pionConnection.LocalDescription().SDP

	if err := steps.Step10_SendSessionAccept(state); err != nil {
		aof.Handshake.LogPrintf("Step10 failed: %v", err)
		return
	}

	// TODO send xmpp heartbeats (BROKEN)
	// TODO: docs: RID++ is ok as it gets passed to new goroutine; but this can be very broken if we add more signaling logic!
	go func() {
		ticker := time.NewTicker(10 * time.Second) // or less than BOSH wait time
		defer ticker.Stop()

		for {
			<-ticker.C
			state.RID++
			requestBody := fmt.Sprintf(`<body rid="%d" sid="%s" xmlns="http://jabber.org/protocol/httpbind">
<iq id="%s:sendIQ" to="%s" type="get" xmlns="jabber:client">
<ping xmlns="urn:xmpp:ping"/></iq></body>`,
				state.RID,
				state.Sid,
				uuid.NewString(),
				serverIP,
			)

			_, err := state.BOSHSender.Send(state.BoshURL, requestBody)
			if err != nil {
				aof.KeepAlive.LogPrintf("[KeepAlive] ERROR: %v", err)
				return
			}
			aof.KeepAlive.LogPrintln("[KeepAlive] Sent keepalive poll.")
		}
	}()

	select {}
}

func ConvertJingleToSDP(state *model.ConnectionState) (string, error) {
	sdp := "v=0\n"
	sdp += "o=- 0 0 IN IP4 0.0.0.0\n"
	sdp += "s=-\n"
	sdp += "t=0 0\n"
	sdp += "a=msid-semantic: WMS\n"
	sdp += "a=group:BUNDLE 0\n"

	// === m=video ===
	sdp += "m=video 9 UDP/TLS/RTP/SAVPF 101\n"
	sdp += "c=IN IP4 0.0.0.0\n"
	sdp += "a=rtpmap:101 VP9/90000\n"
	sdp += "a=fmtp:101 profile-id=0 max-fr=30;max-fs=12266\n"
	sdp += "a=rtcp:9 IN IP4 0.0.0.0\n"

	// RTCP Feedback (VP9 SVC safe set)
	sdp += "a=rtcp-fb:101 ccm fir\n"
	sdp += "a=rtcp-fb:101 nack\n"
	sdp += "a=rtcp-fb:101 nack pli\n"
	sdp += "a=rtcp-fb:101 transport-cc\n"

	// Header extensions (must match Jitsi offer)
	sdp += "a=extmap:3 http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time\n"
	sdp += "a=extmap:5 http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01\n"

	sdp += "a=setup:actpass\n"
	sdp += "a=mid:0\n"
	sdp += "a=recvonly\n"
	sdp += fmt.Sprintf("a=ice-ufrag:%s\n", state.ICEUfrag)
	sdp += fmt.Sprintf("a=ice-pwd:%s\n", state.ICEPwd)
	sdp += fmt.Sprintf("a=fingerprint:sha-256 %s\n", state.Fingerprint)
	sdp += "a=ice-options:trickle\n"
	sdp += "a=rtcp-mux\n"
	sdp += "a=extmap-allow-mixed\n"

	return sdp, nil
}
