package internal

import (
	"fmt"
	"strings"

	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench/pkg/gcc"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/pionutil"
)

func newAPI(clientIP string, impairment *pionutil.ImpairmentBinding, extra ...pionutil.Option) (*webrtc.API, error) {
	opts := append([]pionutil.Option{}, extra...)
	if impairment != nil {
		opts = append(opts, pionutil.WithImpairment(impairment))
	}
	return pionutil.NewAPI(clientIP, 98, opts...)
}

// StartSimplePublisher creates a sendonly PeerConnection without SVC or GCC.
func StartSimplePublisher(l *log.Logger, clientIP string, impairment *pionutil.ImpairmentBinding) (
	*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error,
) {
	api, err := newAPI(clientIP, impairment)
	if err != nil {
		return nil, nil, "", err
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}
	pionutil.RegisterLoggingCallbacks(l, pc)

	track, err := pionutil.NewVP9SampleTrack("ivf")
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
	impairment *pionutil.ImpairmentBinding,
) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticRTP, func() int, string, error) {
	factories, getBitrate, err := gcc.SenderFactories(initialBitrateBps)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("build interceptors: %w", err)
	}

	api, err := newAPI(clientIP, impairment,
		pionutil.WithAPIOption(webrtc.WithInterceptorRegistry(gcc.BuildRegistry(factories))),
	)
	if err != nil {
		return nil, nil, nil, "", err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}
	pionutil.RegisterLoggingCallbacks(l, pc)

	track, err := pionutil.NewVP9RTPTrack("ivf")
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
	impairment *pionutil.ImpairmentBinding,
) (*webrtc.PeerConnection, string, error) {
	api, err := newAPI(clientIP, impairment)
	if err != nil {
		return nil, "", err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", fmt.Errorf("NewPeerConnection: %w", err)
	}
	pionutil.RegisterLoggingCallbacks(l, pc)

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
