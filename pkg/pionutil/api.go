package pionutil

import (
	"fmt"
	"net"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/transport/v4/vnet"
	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench/pkg/log"
)

type apiConfig struct {
	forceActiveDTLS bool
	extraOpts       []func(*webrtc.API)
	impairment      *ImpairmentBinding
}

type ImpairmentBinding struct {
	Router  *ImpairmentRouter
	Profile *ImpairmentProfile
}

type ImpairmentRouter struct {
	router   *vnet.Router
	proxy    *impairmentProxy
	serverIP string
}

func NewImpairmentRouter(serverIP string, serverPort int) (*ImpairmentRouter, error) {
	if serverPort < 0 || serverPort > 65535 {
		return nil, fmt.Errorf("impairment: serverPort %d out of range [1,65535]", serverPort)
	}
	parsedIP := net.ParseIP(serverIP)
	if parsedIP == nil {
		return nil, fmt.Errorf("impairment: invalid serverIP %q", serverIP)
	}

	router, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:          "rtcbench-wan",
		CIDR:          "0.0.0.0/0",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		return nil, fmt.Errorf("vnet.NewRouter: %w", err)
	}

	proxy, err := newImpairmentProxy(router, parsedIP, serverPort)
	if err != nil {
		return nil, err
	}

	return &ImpairmentRouter{
		router:   router,
		proxy:    proxy,
		serverIP: serverIP,
	}, nil
}

func (r *ImpairmentRouter) Start() error {
	return r.router.Start()
}

func (r *ImpairmentRouter) Stop() error {
	if r.proxy != nil {
		_ = r.proxy.Close()
	}
	return r.router.Stop()
}

type Option func(*apiConfig)

func WithForceActiveDTLS() Option {
	return func(c *apiConfig) { c.forceActiveDTLS = true }
}

func WithAPIOption(opt func(*webrtc.API)) Option {
	return func(c *apiConfig) { c.extraOpts = append(c.extraOpts, opt) }
}

func WithImpairment(b *ImpairmentBinding) Option {
	return func(c *apiConfig) { c.impairment = b }
}

func NewSettingEngineMutator(b *ImpairmentBinding) (func(*webrtc.SettingEngine), error) {
	clientNet, err := buildImpairmentNet(b)
	if err != nil {
		return nil, err
	}
	return func(se *webrtc.SettingEngine) {
		se.SetNet(clientNet)
	}, nil
}

func NewAPI(clientIP string, vp9PT uint8, opts ...Option) (*webrtc.API, error) {
	var cfg apiConfig
	for _, o := range opts {
		o(&cfg)
	}

	se := webrtc.SettingEngine{}
	se.SetSRTPProtectionProfiles(dtls.SRTP_AEAD_AES_128_GCM)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	if cfg.forceActiveDTLS {
		se.SetAnsweringDTLSRole(webrtc.DTLSRoleClient)
	}

	if cfg.impairment == nil {
		bindAddr := clientIP
		if bindAddr == "" {
			bindAddr = "0.0.0.0"
		}
		conn, err := net.ListenPacket("udp4", fmt.Sprintf("%s:0", bindAddr))
		if err != nil {
			return nil, fmt.Errorf("bind UDP: %w", err)
		}
		se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	} else {
		clientNet, err := buildImpairmentNet(cfg.impairment)
		if err != nil {
			return nil, err
		}
		se.SetNet(clientNet)
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

func buildImpairmentNet(b *ImpairmentBinding) (*vnet.Net, error) {
	if b == nil || b.Router == nil || b.Profile == nil {
		return nil, fmt.Errorf("impairment binding requires Router and Profile")
	}
	clientNet, err := buildParticipantNet(b.Router.router, b.Profile, allocateVirtualIP())
	if err != nil {
		return nil, fmt.Errorf("buildParticipantNet: %w", err)
	}
	return clientNet, nil
}
