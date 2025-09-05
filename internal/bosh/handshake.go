package bosh

import (
	"fmt"
	"net/url"
	"time"

	"call.zip/internal/aofconf"
	"call.zip/internal/bosh/httpxml"
	"call.zip/internal/bosh/model"
	"call.zip/internal/bosh/steps"
	"call.zip/internal/sdp_tmpl"
	"github.com/google/uuid"
)

func initialRid() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func PerformHandshake(aof *aofconf.Config, serverIP, clientIP, confName, viewerName string, instantReplay bool) {
	state := &model.ConnectionState{
		StepsAOF:      aof.Steps,
		PionAOF:       aof.Pion,
		PktRecvAOF:    aof.PktRecv,
		NetStatsAOF:   aof.NetStats,
		BOSHSender:    httpxml.NewBOSHSender(aof.BOSH),
		BoshURL:       fmt.Sprintf("https://%s/http-bind", serverIP),
		RoomName:      confName,
		MachineUID:    uuid.NewString(),
		RID:           initialRid(),
		Nickname:      viewerName,
		LANServerIP:   serverIP,
		LANClientIP:   clientIP,
		LogsDir:       aof.LogsDir,
		DumpPackets:   aof.DumpPackets,
		InstantReplay: instantReplay,
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

	sdp, err := sdp_tmpl.RenderSDP(sdp_tmpl.SDPState{
		ICEUfrag:      state.ICEUfrag,
		ICEPwd:        state.ICEPwd,
		Fingerprint:   state.Fingerprint,
		InstantReplay: state.InstantReplay,
	})
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
