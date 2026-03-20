package internal

import (
	"fmt"
	"net"
	"strings"

	"call.zip/pkg/log"
	"github.com/pion/dtls/v2"
	"github.com/pion/ice/v2"
	"github.com/pion/webrtc/v3"
)

// NewSharedUDPMux creates a single UDP socket and ICE mux that can be
// shared across all PeerConnections. This avoids binding one kernel
// socket per viewer, reducing file descriptors and kernel buffer memory
// from O(N) to O(1).
func NewSharedUDPMux(clientIP string) (ice.UDPMux, net.PacketConn, error) {
	bindAddr := clientIP
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}
	conn, err := net.ListenPacket("udp4", fmt.Sprintf("%s:0", bindAddr))
	if err != nil {
		return nil, nil, fmt.Errorf("bind UDP: %w", err)
	}
	mux := webrtc.NewICEUDPMux(nil, conn)
	return mux, conn, nil
}

func newPionAPI(mux ice.UDPMux) *webrtc.API {
	se := webrtc.SettingEngine{}
	se.SetSRTPProtectionProfiles(dtls.SRTP_AEAD_AES_128_GCM)
	se.SetICEUDPMux(mux)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})

	m := &webrtc.MediaEngine{}
	m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		PayloadType: 98,
	}, webrtc.RTPCodecTypeVideo)

	return webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
	)
}

func registerLoggingCallbacks(l *log.Logger, pc *webrtc.PeerConnection) {
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		l.Infof("[pion] ICEConnectionState: %s", s)
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			l.Infof("[pion] local ICE candidate: %s", c.ToJSON().Candidate)
		}
	})
}

// StartPionPublisher creates a PeerConnection in sendonly mode and returns the
// PC, the local video track (for IVF streaming), and the offer SDP.
func StartPionPublisher(
	l *log.Logger,
	mux ice.UDPMux,
) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error) {
	api := newPionAPI(mux)

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}

	registerLoggingCallbacks(l, pc)

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

	l.Infof("[pion] publisher offer SDP ready")
	return pc, track, pc.LocalDescription().SDP, nil
}

// StartPionSubscriber creates a PeerConnection in recvonly mode, sets the
// Janus offer as the remote description, and returns the PC and answer SDP.
// The caller is responsible for setting pc.OnTrack before media flows.
func StartPionSubscriber(
	l *log.Logger,
	mux ice.UDPMux,
	janusOfferSDP string,
) (*webrtc.PeerConnection, string, error) {
	api := newPionAPI(mux)

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}

	registerLoggingCallbacks(l, pc)

	if _, err := pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		return nil, "", fmt.Errorf("AddTransceiver: %w", err)
	}

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

	l.Infof("[pion] subscriber answer SDP ready")
	return pc, pc.LocalDescription().SDP, nil
}
