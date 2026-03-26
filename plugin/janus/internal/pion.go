package internal

import (
	"fmt"
	"net"
	"strings"

	"call.zip/pkg/gcc"
	"call.zip/pkg/log"
	"github.com/pion/dtls/v2"
	"github.com/pion/webrtc/v3"
)

func newPionAPI(clientIP string, extraOpts ...func(*webrtc.API)) (*webrtc.API, error) {
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
		PayloadType: 98,
	}, webrtc.RTPCodecTypeVideo)

	opts := []func(*webrtc.API){
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
	}
	opts = append(opts, extraOpts...)

	return webrtc.NewAPI(opts...), nil
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

// StartSimplePublisher creates a sendonly PeerConnection without SVC or GCC.
func StartSimplePublisher(l *log.Logger, clientIP string) (
	*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error,
) {
	api, err := newPionAPI(clientIP)
	if err != nil {
		return nil, nil, "", err
	}
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

// StartPionPublisher creates a PeerConnection in sendonly mode with GCC
// bandwidth estimation. Returns the PC, the RTP track, a function to get
// the current bandwidth estimate, and the offer SDP.
func StartPionPublisher(
	l *log.Logger,
	clientIP string,
	initialBitrateBps int,
) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticRTP, func() int, string, error) {
	factories, getBitrate, err := gcc.SenderFactories(initialBitrateBps)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("build interceptors: %w", err)
	}

	api, err := newPionAPI(clientIP, webrtc.WithInterceptorRegistry(gcc.BuildRegistry(factories)))
	if err != nil {
		return nil, nil, nil, "", err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}

	registerLoggingCallbacks(l, pc)

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		"video", "ivf",
	)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("NewTrackLocalStaticRTP: %w", err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		return nil, nil, nil, "", fmt.Errorf("AddTrack: %w", err)
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("CreateOffer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return nil, nil, nil, "", fmt.Errorf("SetLocalDescription: %w", err)
	}

	l.Infof("[pion] publisher offer SDP ready (GCC enabled)")
	return pc, track, getBitrate, pc.LocalDescription().SDP, nil
}

// StartPionSubscriber creates a PeerConnection in recvonly mode, sets the
// Janus offer as the remote description, and returns the PC and answer SDP.
// The caller is responsible for setting pc.OnTrack before media flows.
func StartPionSubscriber(
	l *log.Logger,
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
