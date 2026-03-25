package internal

import (
	"fmt"
	"net"

	"call.zip/pkg/log"
	"github.com/pion/dtls/v2"
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
	), nil
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

// StartPublisher creates a PeerConnection in sendonly mode and returns the
// PC, the local video track, and the offer SDP.
func StartPublisher(
	l *log.Logger,
	clientIP string,
) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error) {
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

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	l.Infof("[pion] publisher offer SDP ready")
	return pc, track, pc.LocalDescription().SDP, nil
}

// StartSubscriber creates a PeerConnection in recvonly mode, creates an offer,
// and returns the PC and offer SDP. The caller should set pc.OnTrack before
// calling SetRemoteDescription with the answer.
func StartSubscriber(
	l *log.Logger,
	clientIP string,
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

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, "", fmt.Errorf("CreateOffer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return nil, "", fmt.Errorf("SetLocalDescription: %w", err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	l.Infof("[pion] subscriber offer SDP ready")
	return pc, pc.LocalDescription().SDP, nil
}
