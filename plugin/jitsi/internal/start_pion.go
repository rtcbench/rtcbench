package jitsi

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/rtcbench/rtcbench/pkg/gcc"
	ivfpkg "github.com/rtcbench/rtcbench/pkg/ivf"
	pionpkg "github.com/rtcbench/rtcbench/pkg/pion"
	"github.com/rtcbench/rtcbench/pkg/viewer"
	"github.com/rtcbench/rtcbench/plugin/jitsi/internal/model"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
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

const initialBitrateBps = 3_500_000

func (c *Client) startPion(state *model.ConnectionState) (*webrtc.PeerConnection, error) {
	state.Log.Infof("[startPion] initializing pion PeerConnection...")

	// Resolve SVC mode for senders.
	var useSVC bool
	var svcCfg ivfpkg.SVCConfig
	var getTargetBitrate func() int
	var apiOpts []pionpkg.Option
	if state.Sender {
		svc := ivfpkg.ResolveSVC(state.CameraPaths, c.svcConfig.Mode, c.svcConfig.SpatialLayers, c.svcConfig.TemporalLayers, initialBitrateBps)
		useSVC = svc.Enabled
		svcCfg = svc.Config
		if useSVC {
			factories, getBitrate, err := gcc.SenderFactories(initialBitrateBps)
			if err != nil {
				return nil, fmt.Errorf("build interceptors: %w", err)
			}
			getTargetBitrate = getBitrate
			apiOpts = append(apiOpts,
				pionpkg.WithAPIOption(webrtc.WithInterceptorRegistry(gcc.BuildRegistry(factories))),
			)
		}
	}

	api, err := pionpkg.NewAPI(state.LANClientIP, 101, apiOpts...)
	if err != nil {
		return nil, err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("create PeerConnection failed: %w", err)
	}

	// Jitsi uses a superset of the standard logging callbacks.
	pionpkg.RegisterLoggingCallbacks(state.Log, pc)
	pc.OnICEGatheringStateChange(func(iceGatheringState webrtc.ICEGatheringState) {
		state.Log.Infof("[pion] ICEGatheringState: %s", iceGatheringState.String())
	})
	pc.OnConnectionStateChange(func(peerConnState webrtc.PeerConnectionState) {
		state.Log.Infof("[pion] PeerConnectionState: %s", peerConnState.String())
	})

	// Build either a viewer (recvonly transceiver) or a sender (local track).
	var svcTrack *webrtc.TrackLocalStaticRTP
	var sampleTrack *webrtc.TrackLocalStaticSample
	if state.Sender {
		if state.FrameSource == nil {
			return nil, errors.New("expected a frame source for sending")
		}
		if useSVC {
			svcTrack, err = pionpkg.NewVP9RTPTrack(state.Nickname)
			if err != nil {
				return nil, fmt.Errorf("NewTrackLocalStaticRTP failed: %w", err)
			}
			if _, err := pc.AddTrack(svcTrack); err != nil {
				return nil, fmt.Errorf("AddTrack failed: %w", err)
			}
		} else {
			sampleTrack, err = pionpkg.NewVP9SampleTrack(state.Nickname)
			if err != nil {
				return nil, fmt.Errorf("NewTrackLocalStaticSample failed: %w", err)
			}
			if _, err := pc.AddTrack(sampleTrack); err != nil {
				return nil, fmt.Errorf("AddTrack failed: %w", err)
			}
		}
		state.Log.Infof("[startPion] sender mode: frame source ready (svc=%v)", useSVC)
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

	// Start send loop after local description is set.
	if state.Sender {
		if useSVC && svcTrack != nil {
			state.Log.Infof("[startPion] SVC config: %d spatial x %d temporal layers", svcCfg.NumSpatialLayers, svcCfg.NumTemporalLayers)
			go ivfpkg.SVCLoopIntoTrack(state.Log, svcTrack, state.FrameSource, svcCfg, getTargetBitrate)
		} else if sampleTrack != nil {
			go ivfpkg.LoopIntoTrack(state.Log, sampleTrack, state.FrameSource)
		}
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
		state.Log.Infof("[pion] OnTrack: %s %s PT=%d", track.Kind(), track.Codec().MimeType, track.PayloadType())
		ssrc := track.SSRC()
		c.pipeline.ViewerManager.HandleTrack(track, receiver, c.pipeline.Publisher, viewer.TrackHandlerOpts{
			StatsBufferSize:    c.statsBufferSize,
			PacketCaptureDir:   c.packetCaptureDir,
			EnableRecording:    c.enableRecording,
			RecordingDirectory: c.recordingDirectory,
			RoomID:             state.RoomName,
			UserID:             state.Nickname,
			Logger:             state.Log,
			SendPLI: func() {
				if writeErr := pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(ssrc)}}); writeErr != nil {
					state.Log.Errorf("[rtcp] WritePLI: %v", writeErr)
				} else {
					state.Log.Infof("[rtcp] sent PLI ssrc=%d", ssrc)
				}
			},
		})
	})

	return pc, nil
}
