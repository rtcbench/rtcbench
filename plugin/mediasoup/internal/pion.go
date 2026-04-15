package internal

import (
	"fmt"

	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench/pkg/gcc"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/pionutil"
)

func newAPI(clientIP string, vp9PT uint8, impairment *pionutil.ImpairmentBinding, extra ...pionutil.Option) (*webrtc.API, error) {
	opts := append([]pionutil.Option{}, extra...)
	if impairment != nil {
		opts = append(opts, pionutil.WithImpairment(impairment))
	}
	return pionutil.NewAPI(clientIP, vp9PT, opts...)
}

// StartSendPCSimple creates a PeerConnection with a VP9 sample track (no SVC/GCC).
func StartSendPCSimple(l *log.Logger, clientIP string, vp9PT uint8, impairment *pionutil.ImpairmentBinding) (
	*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, string, error,
) {
	api, err := newAPI(clientIP, vp9PT, impairment)
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

	l.Infof("[pion] send offer SDP ready")
	return pc, track, pc.LocalDescription().SDP, nil
}

// StartSendPC creates a PeerConnection with a VP9 send track and GCC
// bandwidth estimation. Returns the PC, RTP track, bandwidth getter, and
// the offer SDP.
func StartSendPC(l *log.Logger, clientIP string, vp9PT uint8, initialBitrateBps int, impairment *pionutil.ImpairmentBinding) (
	*webrtc.PeerConnection, *webrtc.TrackLocalStaticRTP, func() int, string, error,
) {
	factories, getBitrate, err := gcc.SenderFactories(initialBitrateBps)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("build interceptors: %w", err)
	}

	api, err := newAPI(clientIP, vp9PT, impairment,
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

	l.Infof("[pion] send offer SDP ready (GCC enabled)")
	return pc, track, getBitrate, pc.LocalDescription().SDP, nil
}

// StartRecvPC creates a PeerConnection for receiving tracks. It forces the
// DTLS answering role to client (active) to work with mediasoup ICE Lite.
func StartRecvPC(l *log.Logger, clientIP string, vp9PT uint8, impairment *pionutil.ImpairmentBinding) (*webrtc.PeerConnection, error) {
	api, err := newAPI(clientIP, vp9PT, impairment, pionutil.WithForceActiveDTLS())
	if err != nil {
		return nil, err
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("NewPeerConnection: %w", err)
	}
	pionutil.RegisterLoggingCallbacks(l, pc)

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
