package jitsi

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	ivfpkg "call.zip/pkg/ivf"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9"
	"call.zip/plugin/jitsi/internal/model"
	"github.com/pion/dtls/v2"
	"github.com/pion/webrtc/v3"
)

func parseIceCredentials(sdp string) (ufrag, pwd, fingerprint string) {
	ufragRe := regexp.MustCompile(`a=ice-ufrag:(.+)`)
	pwdRe := regexp.MustCompile(`a=ice-pwd:(.+)`)
	fpRe := regexp.MustCompile(`a=fingerprint:[^\s]+ (.+)`)

	if m := ufragRe.FindStringSubmatch(sdp); len(m) > 1 {
		ufrag = strings.TrimSpace(m[1])
	}
	if m := pwdRe.FindStringSubmatch(sdp); len(m) > 1 {
		pwd = strings.TrimSpace(m[1])
	}
	if m := fpRe.FindStringSubmatch(sdp); len(m) > 1 {
		fingerprint = strings.TrimSpace(m[1])
	}
	return
}

// Optional: handy to populate pointer fields for logging/debugging if you want.
func parseLocalVideoMSID(sdp string) (primarySSRC, msid string) {
	// Find first SSRC line, then try to find its msid line.
	// Example:
	//   a=ssrc:3816515605 cname:...
	//   a=ssrc:3816515605 msid:- 52305b34-...
	reSSRC := regexp.MustCompile(`(?m)^a=ssrc:(\d+)\s+`)
	m := reSSRC.FindStringSubmatch(sdp)
	if len(m) != 2 {
		return "", ""
	}
	primarySSRC = strings.TrimSpace(m[1])

	reMSID := regexp.MustCompile(fmt.Sprintf(`(?m)^a=ssrc:%s\s+msid:(.+)\s*$`, regexp.QuoteMeta(primarySSRC)))
	m2 := reMSID.FindStringSubmatch(sdp)
	if len(m2) != 2 {
		return primarySSRC, ""
	}
	msid = strings.TrimSpace(m2[1])
	return primarySSRC, msid
}

func (c *Client) startPion(state *model.ConnectionState, ivf *vp9.IvfSegmenter) (*webrtc.PeerConnection, error) {
	state.Log.Infof("[startPion] initializing pion PeerConnection...")

	conn, err := net.ListenPacket("udp4", fmt.Sprintf("%s:0", state.LANClientIP))
	if err != nil {
		return nil, fmt.Errorf("failed to bind UDP: %w", err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr)
	state.Log.Infof("[startPion] listening on UDP %s:%d", addr.IP, addr.Port)

	se := webrtc.SettingEngine{}
	se.SetSRTPProtectionProfiles(dtls.SRTP_AEAD_AES_128_GCM)
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})

	m := &webrtc.MediaEngine{}

	// VP9 only (no RTX)
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
		state.Log.Infof("[pion] ICEConnectionState: %s", iceConnState.String())
	})
	pc.OnICEGatheringStateChange(func(iceGathererState webrtc.ICEGathererState) {
		state.Log.Infof("[pion] ICEGatheringState: %s", iceGathererState.String())
	})
	pc.OnConnectionStateChange(func(peerConnState webrtc.PeerConnectionState) {
		state.Log.Infof("[pion] PeerConnectionState: %s", peerConnState.String())
	})
	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand != nil {
			state.Log.Infof("[pion] local ICE candidate: %s", cand.ToJSON().Candidate)
		}
	})

	// Build either a viewer (recvonly transceiver) or a sender (local track).
	var localVideoTrack *webrtc.TrackLocalStaticSample
	if state.Sender {
		if state.FrameSource == nil {
			return nil, errors.New("expected a frame source for sending")
		}
		localVideoTrack, err = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{
				MimeType:    webrtc.MimeTypeVP9,
				ClockRate:   90000,
				SDPFmtpLine: "profile-id=0",
			},
			"video",
			state.Nickname, // unique per sender; avoids "MSID already used" rejection from Jicofo
		)
		if err != nil {
			return nil, fmt.Errorf("NewTrackLocalStaticSample failed: %w", err)
		}
		if _, err := pc.AddTrack(localVideoTrack); err != nil {
			return nil, fmt.Errorf("AddTrack failed: %w", err)
		}
		state.Log.Infof("[startPion] sender mode: frame source ready")
	} else {
		// Viewer mode: ensure we have a recvonly video transceiver.
		if _, err := pc.AddTransceiverFromKind(
			webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
		); err != nil {
			return nil, fmt.Errorf("AddTransceiver (recvonly) failed: %w", err)
		}
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  state.RemoteSDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("SetRemoteDescription failed: %w", err)
	}
	state.Log.Debugf("[after SetRemoteDescription] received SDP offer:\n%s", pc.RemoteDescription().SDP)

	// Your reconstructed offer uses a=mid:0 for the video m-line, so SDPMid must be "0".
	mid0 := "0"

	if len(state.Candidates) == 0 {
		staticCandidate := fmt.Sprintf("candidate:1 1 udp 2130706431 %s 10000 typ host", state.LANServerIP)
		if err := pc.AddICECandidate(webrtc.ICECandidateInit{
			Candidate: staticCandidate,
			SDPMid:    &mid0,
		}); err != nil {
			return nil, fmt.Errorf("AddICECandidate (fallback) failed: %w", err)
		}
		state.Log.Infof("[pion] added static remote candidate: %s", staticCandidate)
	} else {
		for _, cand := range state.Candidates {
			candidateLine := fmt.Sprintf("candidate:%s %d %s %d %s %d typ %s",
				cand.Foundation, cand.Component, cand.Protocol, cand.Priority,
				cand.IP, cand.Port, cand.Type,
			)
			if err := pc.AddICECandidate(webrtc.ICECandidateInit{
				Candidate: candidateLine,
				SDPMid:    &mid0,
			}); err != nil {
				return nil, fmt.Errorf("AddICECandidate failed: %w", err)
			}
			state.Log.Infof("[pion] added remote candidate: %s", candidateLine)
		}
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("CreateAnswer failed: %w", err)
	}
	// Keep this if you've actually observed "actpass" leaking into an answer in your environment.
	answer.SDP = strings.Replace(answer.SDP, "a=setup:actpass", "a=setup:active", 1)

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("SetLocalDescription failed: %w", err)
	}
	<-gatherComplete

	state.Log.Debugf("[after SetLocalDescription] generated SDP answer:\n%s", pc.LocalDescription().SDP)

	// Start IVF pumping after local description is set.
	if state.Sender && localVideoTrack != nil {
		go ivfpkg.LoopIntoTrack(state.Log, localVideoTrack, state.FrameSource)
	}

	ufrag, pwd, fingerprint := parseIceCredentials(pc.LocalDescription().SDP)
	state.LocalSDP = pc.LocalDescription().SDP
	state.ICEUfrag = ufrag
	state.ICEPwd = pwd
	state.Fingerprint = fingerprint

	// Optional: populate pointer fields if you created them; do not deref nil pointers.
	if state.Sender {
		primarySSRC, msid := parseLocalVideoMSID(state.LocalSDP)
		if state.SenderSSRC != nil && primarySSRC != "" {
			*state.SenderSSRC = primarySSRC
		}
		if state.SenderMSID != nil && msid != "" {
			*state.SenderMSID = msid
		}
		state.Log.Infof("[startPion] sender mode: parsed primarySSRC=%q msid=%q", primarySSRC, msid)
	}

	state.Log.Infof("[startPion] local SDP ufrag=%s pwd=%s fingerprint=%s", ufrag, pwd, fingerprint)
	state.Log.Infof("[startPion] pion PeerConnection ready with SDP answer generated.")

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go func() {
			cfg := &viewer.Config{
				PacketsPerSample:  1000,
				VP9RTPPayloadType: 101,
				TrackBufferSize:   1500,
				StatsBufferSize:   c.statsBufferSize,
			}

			if _, err := c.botManager.SpawnViewer(track, receiver, state.Nickname, cfg, ivf); err != nil {
				panic(err) // TODO don't panic (manager refactor)
			}

			select {} // TODO manager refactor
		}()
	})

	return pc, nil
}
