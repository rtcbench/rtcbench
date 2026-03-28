package internal

import (
	"fmt"
	"net"

	"call.zip/pkg/gcc"
	"call.zip/pkg/log"
	"github.com/pion/dtls/v3"
	"github.com/pion/webrtc/v4"
)

// newPionAPI creates a pion API with a single shared UDP socket and VP9
// registered at the given payload type. If forceActiveRole is true, the
// answering DTLS role is forced to client (active).
func newPionAPI(clientIP string, vp9PayloadType uint8, forceActiveRole bool, extraOpts ...func(*webrtc.API)) (*webrtc.API, error) {
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
	if forceActiveRole {
		se.SetAnsweringDTLSRole(webrtc.DTLSRoleClient)
	}

	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		PayloadType: webrtc.PayloadType(vp9PayloadType),
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, fmt.Errorf("register VP9 codec: %w", err)
	}
	m.RegisterFeedback(webrtc.RTCPFeedback{Type: webrtc.TypeRTCPFBTransportCC}, webrtc.RTPCodecTypeVideo)
	m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"}, webrtc.RTPCodecTypeVideo)

	opts := []func(*webrtc.API){
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
	}
	opts = append(opts, extraOpts...)

	return webrtc.NewAPI(opts...), nil
}

// StartSendPCSimple creates a PeerConnection with a VP9 sample track (no SVC/GCC).
func StartSendPCSimple(l *log.Logger, clientIP string, vp9PT uint8) (
	*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error,
) {
	api, err := newPionAPI(clientIP, vp9PT, false)
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

	l.Infof("[pion] send offer SDP ready")
	return pc, track, pc.LocalDescription().SDP, nil
}

// StartSendPC creates a PeerConnection with a VP9 send track and GCC
// bandwidth estimation. Returns the PC, RTP track, bandwidth getter, and
// the offer SDP.
func StartSendPC(l *log.Logger, clientIP string, vp9PT uint8, initialBitrateBps int) (
	*webrtc.PeerConnection, *webrtc.TrackLocalStaticRTP, func() int, string, error,
) {
	factories, getBitrate, err := gcc.SenderFactories(initialBitrateBps)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("build interceptors: %w", err)
	}

	api, err := newPionAPI(clientIP, vp9PT, false, webrtc.WithInterceptorRegistry(gcc.BuildRegistry(factories)))
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

	l.Infof("[pion] send offer SDP ready (GCC enabled)")
	return pc, track, getBitrate, pc.LocalDescription().SDP, nil
}

// StartRecvPC creates a PeerConnection for receiving tracks. It forces the
// DTLS answering role to client (active) to work with mediasoup ICE Lite.
func StartRecvPC(l *log.Logger, clientIP string, vp9PT uint8) (*webrtc.PeerConnection, error) {
	api, err := newPionAPI(clientIP, vp9PT, true)
	if err != nil {
		return nil, err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("NewPeerConnection: %w", err)
	}
	registerLoggingCallbacks(l, pc)

	l.Infof("[pion] recv PeerConnection created")
	return pc, nil
}

// NegotiateRecvSDP sets the given offer SDP as remote description, creates an
// answer forcing DTLS client role, and returns the answer SDP.
func NegotiateRecvSDP(pc *webrtc.PeerConnection, offerSDP string) (string, error) {
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		return "", fmt.Errorf("SetRemoteDescription: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("CreateAnswer: %w", err)
	}
	// DTLS client role is forced via SettingEngine.SetAnsweringDTLSRole.
	if err := pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("SetLocalDescription: %w", err)
	}
	return pc.LocalDescription().SDP, nil
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
