package jitsi

import (
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"call.zip/pkg/vp9"
	"call.zip/plugin/jitsi/internal/bot"
	"call.zip/plugin/jitsi/internal/model"
	"github.com/pion/dtls/v2"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfreader"
)

// loopIVFIntoTrack continuously re-reads an IVF file and writes VP9 frames into the provided track.
// It paces frames using IVF timestamps when possible, with a conservative fallback.
func loopIVFIntoTrack(
	logf func(format string, args ...any),
	track *webrtc.TrackLocalStaticSample,
	senderIVFPaths []string,
) {
	path := 0
	for {
		f, err := os.Open(senderIVFPaths[path])
		if err != nil {
			logf("[ivf] open failed: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		r, header, err := ivfreader.NewWith(f)
		if err != nil {
			_ = f.Close()
			logf("[ivf] NewWith failed: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		num := header.TimebaseNumerator
		den := header.TimebaseDenominator

		// Conservative fallback: do NOT assume 1 tick == 1 frame.
		// If timestamps are usable, we compute durations from delta ticks.
		defaultDur := 33 * time.Millisecond

		var prevTS *uint64

		for {
			frame, fh, err := r.ParseNextFrame()
			if err != nil {
				logf("[ivf] ParseNextFrame error: %v", err)
				break
			}
			// EOF: ivfreader returns all-nil.
			if frame == nil && fh == nil {
				break
			}

			dur := defaultDur
			if prevTS != nil && fh.Timestamp > *prevTS && num != 0 && den != 0 {
				ticks := fh.Timestamp - *prevTS
				dur = time.Duration(int64(time.Second) * int64(ticks) * int64(num) / int64(den))
				if dur <= 0 {
					dur = defaultDur
				}
			}

			ts := fh.Timestamp
			prevTS = &ts

			if err := track.WriteSample(media.Sample{
				Data:     frame,
				Duration: dur,
			}); err != nil {
				logf("[ivf] WriteSample failed: %v", err)
				_ = f.Close()
				return
			}

			time.Sleep(dur)
		}

		_ = f.Close()

		path++
		path %= len(senderIVFPaths)
		// loop back and open the next IVF from the beginning
	}
}

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
		state.PionAOF.LogPrintf("[pion] ICEConnectionState: %s", iceConnState.String())
	})
	pc.OnICEGatheringStateChange(func(iceGathererState webrtc.ICEGathererState) {
		state.PionAOF.LogPrintf("[pion] ICEGatheringState: %s", iceGathererState.String())
	})
	pc.OnConnectionStateChange(func(peerConnState webrtc.PeerConnectionState) {
		state.PionAOF.LogPrintf("[pion] PeerConnectionState: %s", peerConnState.String())
	})
	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand != nil {
			state.PionAOF.LogPrintf("[pion] Local ICE candidate: %s", cand.ToJSON().Candidate)
		}
	})

	// Build either a viewer (recvonly transceiver) or a sender (local track).
	var localVideoTrack *webrtc.TrackLocalStaticSample
	if state.Sender {
		if len(state.SenderIVFPaths) == 0 {
			return nil, errors.New("expected at least 1 IVF file path to send")
		}
		localVideoTrack, err = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{
				MimeType:    webrtc.MimeTypeVP9,
				ClockRate:   90000,
				SDPFmtpLine: "profile-id=0",
			},
			"video",
			"ivf",
		)
		if err != nil {
			return nil, fmt.Errorf("NewTrackLocalStaticSample failed: %w", err)
		}
		if _, err := pc.AddTrack(localVideoTrack); err != nil {
			return nil, fmt.Errorf("AddTrack failed: %w", err)
		}
		state.PionAOF.LogPrintf("[startPion] Sender mode: will stream IVF starting from %s", state.SenderIVFPaths[0])
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
	state.PionAOF.LogPrintf("[after SetRemoteDescription] Received SDP Offer:\n%s", pc.RemoteDescription().SDP)

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
		state.PionAOF.LogPrintf("[pion] ✅ Added static remote candidate: %s", staticCandidate)
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
			state.PionAOF.LogPrintf("[pion] Added remote candidate: %s", candidateLine)
		}
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("CreateAnswer failed: %w", err)
	}
	// Keep this if you’ve actually observed "actpass" leaking into an answer in your environment.
	answer.SDP = strings.Replace(answer.SDP, "a=setup:actpass", "a=setup:active", 1)

	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("SetLocalDescription failed: %w", err)
	}

	state.PionAOF.LogPrintf("[after SetLocalDescription] Generated SDP Answer:\n%s", pc.LocalDescription().SDP)

	// Start IVF pumping after local description is set.
	if state.Sender && localVideoTrack != nil {
		go loopIVFIntoTrack(state.PionAOF.LogPrintf, localVideoTrack, state.SenderIVFPaths)
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
		state.PionAOF.LogPrintf("[startPion] Sender mode: parsed primarySSRC=%q msid=%q", primarySSRC, msid)
	}

	state.PionAOF.LogPrintf("[startPion] ✅ Local SDP ufrag=%s pwd=%s fingerprint=%s", ufrag, pwd, fingerprint)
	state.PionAOF.LogPrintln("[startPion] ✅ pion PeerConnection ready with SDP answer generated.")

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go func() {
			cfg := &bot.ViewerConfig{
				PacketsPerSample:  1000,
				VP9RTPPayloadType: 101,
				TrackBufferSize:   1500,
			}

			if _, err := c.botManager.SpawnViewer(track, receiver, state.Nickname, cfg, ivf); err != nil {
				panic(err) // TODO don't panic (manager refactor)
			}

			select {} // TODO manager refactor
		}()
	})

	return pc, nil
}
