package internal

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pion/dtls/v2"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfreader"
)

func newPionAPI(clientIP string) (*webrtc.API, error) {
	bindAddr := clientIP
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}
	conn, err := net.ListenPacket("udp4", fmt.Sprintf("%s:0", bindAddr))
	if err != nil {
		return nil, fmt.Errorf("bind UDP: %w", err)
	}

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

	return webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
	), nil
}

func registerLoggingCallbacks(logf func(string, ...any), pc *webrtc.PeerConnection) {
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		logf("[pion] ICEConnectionState: %s", s)
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			logf("[pion] local ICE candidate: %s", c.ToJSON().Candidate)
		}
	})
}

// StartPionPublisher creates a PeerConnection in sendonly mode, creates an
// SDP offer and sets it as the local description.  Returns the PC, the local
// video track (so the caller can start writing IVF after the remote answer is
// applied), and the offer SDP.
func StartPionPublisher(
	logf func(string, ...any),
	clientIP string,
	ivfPaths []string,
) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error) {
	api, err := newPionAPI(clientIP)
	if err != nil {
		return nil, nil, "", err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}

	registerLoggingCallbacks(logf, pc)

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		"video", "ivf",
	)
	if err != nil {
		return nil, nil, "", fmt.Errorf("NewTrackLocalStaticSample: %w", err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		return nil, nil, "", fmt.Errorf("AddTrack: %w", err)
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, nil, "", fmt.Errorf("CreateOffer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return nil, nil, "", fmt.Errorf("SetLocalDescription: %w", err)
	}

	logf("[pion] publisher offer SDP ready")
	return pc, track, pc.LocalDescription().SDP, nil
}

// StartPionSubscriber creates a PeerConnection in recvonly mode, sets the
// Janus offer as the remote description, creates an answer and sets it as
// the local description.  Returns the PC and the answer SDP.
func StartPionSubscriber(
	logf func(string, ...any),
	clientIP string,
	janusOfferSDP string,
) (*webrtc.PeerConnection, string, error) {
	api, err := newPionAPI(clientIP)
	if err != nil {
		return nil, "", err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}

	registerLoggingCallbacks(logf, pc)

	if _, err := pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		return nil, "", fmt.Errorf("AddTransceiver: %w", err)
	}

	// Read-and-discard incoming RTP so the buffer never fills up.
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		logf("[pion] OnTrack: %s %s", track.Kind(), track.Codec().MimeType)
		buf := make([]byte, 1500)
		for {
			if _, _, err := track.Read(buf); err != nil {
				return
			}
		}
	})

	remote := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: janusOfferSDP}
	if err := pc.SetRemoteDescription(remote); err != nil {
		return nil, "", fmt.Errorf("SetRemoteDescription: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, "", fmt.Errorf("CreateAnswer: %w", err)
	}
	// Ensure we're the DTLS client (active), not actpass.
	answer.SDP = strings.Replace(answer.SDP, "a=setup:actpass", "a=setup:active", 1)

	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, "", fmt.Errorf("SetLocalDescription: %w", err)
	}

	logf("[pion] subscriber answer SDP ready")
	return pc, pc.LocalDescription().SDP, nil
}

// addStaticCandidate adds a static host ICE candidate pointing at serverIP:10000.
func AddStaticCandidate(pc *webrtc.PeerConnection, serverIP string) error {
	mid0 := "0"
	candidate := fmt.Sprintf("candidate:1 1 udp 2130706431 %s 10000 typ host", serverIP)
	return pc.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: candidate,
		SDPMid:    &mid0,
	})
}

// LoopIVFIntoTrack continuously re-reads the IVF files in order and writes
// VP9 frames into the track, pacing by the IVF timestamps.
func LoopIVFIntoTrack(
	logf func(string, ...any),
	track *webrtc.TrackLocalStaticSample,
	ivfPaths []string,
) {
	defaultDur := 33 * time.Millisecond
	idx := 0
	for {
		f, err := os.Open(ivfPaths[idx])
		if err != nil {
			logf("[ivf] open %s: %v", ivfPaths[idx], err)
			time.Sleep(2 * time.Second)
			continue
		}

		r, header, err := ivfreader.NewWith(f)
		if err != nil {
			_ = f.Close()
			logf("[ivf] NewWith: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		num := header.TimebaseNumerator
		den := header.TimebaseDenominator
		var prevTS *uint64

		for {
			frame, fh, err := r.ParseNextFrame()
			if err != nil || (frame == nil && fh == nil) {
				break
			}

			dur := defaultDur
			if prevTS != nil && fh.Timestamp > *prevTS && num != 0 && den != 0 {
				ticks := fh.Timestamp - *prevTS
				d := time.Duration(int64(time.Second) * int64(ticks) * int64(num) / int64(den))
				if d > 0 {
					dur = d
				}
			}
			ts := fh.Timestamp
			prevTS = &ts

			if err := track.WriteSample(media.Sample{Data: frame, Duration: dur}); err != nil {
				logf("[ivf] WriteSample: %v", err)
				_ = f.Close()
				return
			}
			time.Sleep(dur)
		}

		_ = f.Close()
		idx = (idx + 1) % len(ivfPaths)
	}
}
