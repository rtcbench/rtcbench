package pion

import (
	"fmt"
	"net"

	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/pion/dtls/v3"
	"github.com/pion/webrtc/v4"
)

type apiConfig struct {
	forceActiveDTLS bool
	extraOpts       []func(*webrtc.API)
}

// Option configures NewAPI behavior.
type Option func(*apiConfig)

// WithForceActiveDTLS forces the answering DTLS role to client (active),
// required for ICE Lite servers like mediasoup.
func WithForceActiveDTLS() Option {
	return func(c *apiConfig) { c.forceActiveDTLS = true }
}

// WithAPIOption appends a raw pion API option (e.g. WithInterceptorRegistry).
func WithAPIOption(opt func(*webrtc.API)) Option {
	return func(c *apiConfig) { c.extraOpts = append(c.extraOpts, opt) }
}

// NewAPI creates a pion WebRTC API with a single shared UDP socket and VP9
// registered at the given payload type, including transport-cc feedback and
// the TWCC header extension. This is the shared core used by Janus (PT=98),
// Mediasoup (dynamic PT), and Jitsi (PT=101).
func NewAPI(clientIP string, vp9PT uint8, opts ...Option) (*webrtc.API, error) {
	var cfg apiConfig
	for _, o := range opts {
		o(&cfg)
	}

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
	if cfg.forceActiveDTLS {
		se.SetAnsweringDTLSRole(webrtc.DTLSRoleClient)
	}

	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		PayloadType: webrtc.PayloadType(vp9PT),
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, fmt.Errorf("register VP9 codec: %w", err)
	}
	m.RegisterFeedback(webrtc.RTCPFeedback{Type: webrtc.TypeRTCPFBTransportCC}, webrtc.RTPCodecTypeVideo)
	m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{
		URI: "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01",
	}, webrtc.RTPCodecTypeVideo)

	apiOpts := []func(*webrtc.API){
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
	}
	apiOpts = append(apiOpts, cfg.extraOpts...)

	return webrtc.NewAPI(apiOpts...), nil
}

// RegisterLoggingCallbacks adds ICE connection state and candidate logging
// to a PeerConnection. Jitsi adds extra callbacks (gathering state, connection
// state) beyond these two.
func RegisterLoggingCallbacks(l *log.Logger, pc *webrtc.PeerConnection) {
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		l.Infof("[pion] ICEConnectionState: %s", s)
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			l.Infof("[pion] local ICE candidate: %s", c.ToJSON().Candidate)
		}
	})
}

// NewVP9SampleTrack creates a VP9 sample-based track for simple (non-SVC)
// senders. The streamID defaults to "ivf"; Jitsi uses the user nickname.
func NewVP9SampleTrack(streamID string) (*webrtc.TrackLocalStaticSample, error) {
	return webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		"video", streamID,
	)
}

// NewVP9RTPTrack creates a VP9 RTP-based track for SVC senders that need
// per-packet control over RTP headers.
func NewVP9RTPTrack(streamID string) (*webrtc.TrackLocalStaticRTP, error) {
	return webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		"video", streamID,
	)
}
