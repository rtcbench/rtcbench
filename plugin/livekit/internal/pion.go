package internal

import (
	"fmt"
	"net"

	"github.com/livekit/protocol/livekit"
	"github.com/pion/dtls/v2"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
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
	// Register VP9 without pinning a payload type — the server will negotiate it.
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP9,
			ClockRate: 90000,
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, fmt.Errorf("RegisterCodec VP9: %w", err)
	}

	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil {
		return nil, fmt.Errorf("RegisterDefaultInterceptors: %w", err)
	}

	return webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(ir),
	), nil
}

// iceConfig converts the ICE servers from a LiveKit JoinResponse to pion config.
func iceConfig(jr *livekit.JoinResponse) webrtc.Configuration {
	servers := make([]webrtc.ICEServer, 0, len(jr.IceServers))
	for _, s := range jr.IceServers {
		srv := webrtc.ICEServer{URLs: s.Urls}
		if s.Username != "" {
			srv.Username = s.Username
			srv.Credential = s.Credential
		}
		servers = append(servers, srv)
	}
	return webrtc.Configuration{ICEServers: servers}
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

// StartPionPublisher creates a sendonly PeerConnection, adds a VP9 track,
// and returns the PC, the local track, the offer SDP, and the track ID
// (which must be used as the cid in AddTrackRequest).
func StartPionPublisher(
	logf func(string, ...any),
	clientIP string,
	joinResp *livekit.JoinResponse,
	trackID string,
) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error) {
	api, err := newPionAPI(clientIP)
	if err != nil {
		return nil, nil, "", err
	}

	pc, err := api.NewPeerConnection(iceConfig(joinResp))
	if err != nil {
		return nil, nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}

	registerLoggingCallbacks(logf, pc)

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9, ClockRate: 90000},
		"camera", trackID,
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

	logf("[pion] publisher offer SDP ready (trackID=%s)", trackID)
	return pc, track, pc.LocalDescription().SDP, nil
}

// StartPionSubscriber creates a recvonly PeerConnection, sets the server's
// offer as the remote description, and returns the PC and the answer SDP.
// The caller sets pc.OnTrack before media flows.
func StartPionSubscriber(
	logf func(string, ...any),
	clientIP string,
	joinResp *livekit.JoinResponse,
	serverOfferSDP string,
) (*webrtc.PeerConnection, string, error) {
	api, err := newPionAPI(clientIP)
	if err != nil {
		return nil, "", err
	}

	pc, err := api.NewPeerConnection(iceConfig(joinResp))
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

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  serverOfferSDP,
	}); err != nil {
		return nil, "", fmt.Errorf("SetRemoteDescription: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, "", fmt.Errorf("CreateAnswer: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, "", fmt.Errorf("SetLocalDescription: %w", err)
	}

	logf("[pion] subscriber answer SDP ready")
	return pc, pc.LocalDescription().SDP, nil
}
