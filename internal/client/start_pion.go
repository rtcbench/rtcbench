package client

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"call.zip/internal/bosh/model"
	"call.zip/internal/bot"
	"github.com/google/uuid"
	"github.com/pion/dtls/v2"
	"github.com/pion/webrtc/v3"
)

func parseIceCredentials(sdp string) (ufrag, pwd, fingerprint string) {
	ufragRe := regexp.MustCompile(`a=ice-ufrag:(.+)`)
	pwdRe := regexp.MustCompile(`a=ice-pwd:(.+)`)
	fpRe := regexp.MustCompile(`a=fingerprint:[^\s]+ (.+)`)

	if m := ufragRe.FindStringSubmatch(sdp); len(m) > 1 {
		ufrag = m[1]
	}
	if m := pwdRe.FindStringSubmatch(sdp); len(m) > 1 {
		pwd = m[1]
	}
	if m := fpRe.FindStringSubmatch(sdp); len(m) > 1 {
		fingerprint = m[1]
	}
	return
}

func (c *Client) startPion(state *model.ConnectionState) (*webrtc.PeerConnection, error) {
	state.PionAOF.LogPrintln("[startPion] Initializing pion PeerConnection...")

	conn, err := net.ListenPacket("udp4", fmt.Sprintf("%s:0", state.LANClientIP))
	if err != nil {
		return nil, fmt.Errorf("failed to bind UDP: %w", err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr)
	state.PionAOF.LogPrintf("[startPion] Listening on UDP %s:%d", addr.IP, addr.Port)

	se := webrtc.SettingEngine{}
	se.SetSRTPProtectionProfiles(dtls.SRTP_AEAD_AES_128_GCM)
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})

	m := &webrtc.MediaEngine{}
	m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		PayloadType: 101,
	}, webrtc.RTPCodecTypeVideo)

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
	)

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("create PeerConnection failed: %w", err)
	}

	pc.OnICEConnectionStateChange(func(iceConnState webrtc.ICEConnectionState) {
		state.PionAOF.LogPrintf("[pion] ICEConnectionState: %s", iceConnState.String())
	})

	pc.OnICEGatheringStateChange(func(iceGathererState webrtc.ICEGathererState) {
		state.PionAOF.LogPrintf("[pion] ICEGatheringState: %s", iceGathererState.String())
	})

	pc.OnConnectionStateChange(func(peerConnState webrtc.PeerConnectionState) {
		state.PionAOF.LogPrintf("[pion] PeerConnectionState: %s", peerConnState.String())
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			state.PionAOF.LogPrintf("[pion] Local ICE candidate: %s", c.ToJSON().Candidate)
		}
	})

	dir := webrtc.RTPTransceiverDirectionRecvonly
	if state.InstantReplay {
		dir = webrtc.RTPTransceiverDirectionSendrecv
	}
	if _, err = pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: dir},
	); err != nil {
		return nil, fmt.Errorf("AddTransceiver failed: %w", err)
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  state.RemoteSDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("SetRemoteDescription failed: %w", err)
	}

	state.PionAOF.LogPrintf("[after SetRemoteDescription] Received SDP Offer:\n%s", pc.RemoteDescription().SDP)

	if len(state.Candidates) == 0 {
		staticCandidate := fmt.Sprintf("candidate:1 1 udp 2130706431 %s 10000 typ host", state.LANServerIP)
		for _, mid := range []string{"video"} {
			if err := pc.AddICECandidate(webrtc.ICECandidateInit{
				Candidate: staticCandidate,
				SDPMid:    &mid,
			}); err != nil {
				return nil, fmt.Errorf("AddICECandidate (fallback) failed: %w", err)
			}
		}
		state.PionAOF.LogPrintf("[pion] ✅ Added static remote candidate: %s", staticCandidate)
	} else {
		for _, cand := range state.Candidates {
			candidateLine := fmt.Sprintf("candidate:%s %d %s %d %s %d typ %s",
				cand.Foundation, cand.Component, cand.Protocol, cand.Priority,
				cand.IP, cand.Port, cand.Type,
			)
			for _, mid := range []string{"video"} {
				if err := pc.AddICECandidate(webrtc.ICECandidateInit{
					Candidate: candidateLine,
					SDPMid:    &mid,
				}); err != nil {
					return nil, fmt.Errorf("AddICECandidate failed: %w", err)
				}
			}
			state.PionAOF.LogPrintf("[pion] Added remote candidate: %s", candidateLine)
		}
	}

	var outSSRC uint32
	var outMSID string
	var pubTrack *webrtc.TrackLocalStaticRTP
	if state.InstantReplay {
		state.PionAOF.LogPrintln("[instant-replay] Attaching local TrackLocalStaticRTP on same PeerConnection (sendrecv)")
		pubTrack, err = webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:    webrtc.MimeTypeVP9,
				ClockRate:   90000,
				SDPFmtpLine: "profile-id=0",
			},
			"video",          // track ID
			uuid.NewString(), // stream ID
		)
		if err != nil {
			return nil, fmt.Errorf("NewTrackLocalStaticRTP failed: %w", err)
		}
		sender, err := pc.AddTrack(pubTrack)
		if err != nil {
			return nil, fmt.Errorf("AddTrack (instant replay) failed: %w", err)
		}
		// drain RTCP so feedback doesn't back up
		go func() {
			pkts := make([]byte, 10000) // TODO made up buffer size
			for {
				if _, _, err := sender.Read(pkts); err != nil {
					return
				}
			}
		}()
		params := sender.GetParameters()
		if len(params.Encodings) > 0 {
			outSSRC = uint32(params.Encodings[0].SSRC)
		}
		outMSID = pubTrack.StreamID()
	}
	state.SenderSSRC = fmt.Sprintf("%d", outSSRC)
	state.SenderMSID = fmt.Sprintf("%s", outMSID)

	answer, err := pc.CreateAnswer(nil)
	answer.SDP = strings.Replace(answer.SDP, "a=setup:actpass", "a=setup:active", 1)
	if err != nil {
		return nil, fmt.Errorf("CreateAnswer failed: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("SetLocalDescription failed: %w", err)
	}

	state.PionAOF.LogPrintf("[after SetLocalDescription] Generated SDP Answer:\n%s", pc.LocalDescription().SDP)

	ufrag, pwd, fingerprint := parseIceCredentials(pc.LocalDescription().SDP)
	state.LocalSDP = pc.LocalDescription().SDP
	state.ICEUfrag = ufrag
	state.ICEPwd = pwd
	state.Fingerprint = fingerprint

	state.PionAOF.LogPrintf("[startPion] ✅ Local SDP ufrag=%s pwd=%s fingerprint=%s", ufrag, pwd, fingerprint)
	state.PionAOF.LogPrintln("[startPion] ✅ pion PeerConnection ready with SDP answer generated.")

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go func() {
			cfg := &bot.ViewerConfig{
				PacketsPerSample:  1000,
				VP9RTPPayloadType: 101,
				TrackBufferSize:   1500,
				PublishTrack:      pubTrack, // nil unless InstantReplay
			}

			if state.InstantReplay {
				cfg.Recorder = bot.New("rec_pkts_"+state.Nickname+".json", 1500*1_000_000, 10_000)
			}

			if _, err := c.botManager.SpawnViewer(track, receiver, state.Nickname, cfg); err != nil {
				panic(err) // TODO don't panic (manager refactor)
			}

			select {} // TODO manager refactor
		}()
	})

	return pc, nil
}
