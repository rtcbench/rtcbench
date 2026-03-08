package livekit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9_stats"
	lk "call.zip/plugin/livekit/internal"
	"github.com/google/uuid"
	"github.com/livekit/protocol/livekit"
	"github.com/pion/webrtc/v3"
)

const (
	PluginID = "livekit"

	cfgServerURL      = "serverURL"
	cfgAPIKey         = "apiKey"
	cfgAPISecret      = "apiSecret"
	cfgAllowInsecure  = "allowInsecureWss"

	viewerPacketsPerSample = 1000
	viewerTrackBufferSize  = 1500
	statsInputChanSize     = 128

	offerTimeout     = 15 * time.Second
	answerTimeout    = 15 * time.Second
	publishedTimeout = 10 * time.Second
)

type Plugin struct {
	serverURL     string
	apiKey        string
	apiSecret     string
	allowInsecure bool
	clientIP      string
	ivfPaths      []string
	viewerManager *viewer.Manager
	publisher     *vp9_stats.Publisher
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, config *call.Config) error {
	cfg := config.Spec.PluginConfig[PluginID].(map[string]any)
	p.serverURL = cfg[cfgServerURL].(string)
	p.apiKey = cfg[cfgAPIKey].(string)
	p.apiSecret = cfg[cfgAPISecret].(string)
	if v, ok := cfg[cfgAllowInsecure].(bool); ok {
		p.allowInsecure = v
	}

	p.clientIP = config.Spec.Network.ClientIP

	if config.Spec.Conference.Cameras.PerRoom > 0 {
		paths, err := ivf.LoadCameraPaths(config.Spec.Conference.Cameras.Directory)
		if err != nil {
			return fmt.Errorf("livekit: %w", err)
		}
		p.ivfPaths = paths
	}

	statsInput := make(chan vp9_stats.VideoQualitySample, statsInputChanSize)
	p.viewerManager = viewer.NewManager(statsInput)
	p.publisher = vp9_stats.NewPublisher(statsInput)
	p.publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		log.Printf("[sub-data-v1] period=%s,sample=%s", period.String(), sample.String())
	})
	go p.publisher.Run()

	return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	p.viewerManager.StopAll()
	p.publisher.Stop()
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	logf := func(format string, args ...any) {
		log.Printf("[livekit][%s][%s] "+format, append([]any{string(role), userID}, args...)...)
	}
	switch role {
	case call.Sender:
		return p.runSender(ctx, logf, roomID, userID)
	case call.Viewer:
		return p.runViewer(ctx, logf, roomID, userID)
	default:
		return call.ErrUnsupportedRole
	}
}

// wireTrickle sets up bidirectional ICE trickle between a PeerConnection and
// the signal channel for a given target (PUBLISHER or SUBSCRIBER).
func wireTrickle(
	ctx context.Context,
	pc *webrtc.PeerConnection,
	sig *lk.SignalClient,
	target livekit.SignalTarget,
	incoming <-chan *livekit.TrickleRequest,
	logf func(string, ...any),
) {
	// Local candidates → signal.
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		b, err := json.Marshal(c.ToJSON())
		if err != nil {
			logf("[trickle] marshal candidate: %v", err)
			return
		}
		if err := sig.Send(&livekit.SignalRequest{
			Message: &livekit.SignalRequest_Trickle{
				Trickle: &livekit.TrickleRequest{
					CandidateInit: string(b),
					Target:        target,
				},
			},
		}); err != nil {
			logf("[trickle] send: %v", err)
		}
	})

	// Remote candidates from signal → PC.
	go func() {
		for {
			select {
			case t, ok := <-incoming:
				if !ok {
					return
				}
				var init webrtc.ICECandidateInit
				if err := json.Unmarshal([]byte(t.CandidateInit), &init); err != nil {
					logf("[trickle] unmarshal remote candidate: %v", err)
					continue
				}
				if err := pc.AddICECandidate(init); err != nil {
					logf("[trickle] AddICECandidate: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (p *Plugin) runSender(ctx context.Context, logf func(string, ...any), roomID, userID string) error {
	if len(p.ivfPaths) == 0 {
		return fmt.Errorf("%w: no IVF files configured for sender", call.ErrCannotJoinRoom)
	}

	token, err := lk.MakeToken(p.apiKey, p.apiSecret, roomID, userID, true, false)
	if err != nil {
		return fmt.Errorf("%w: make token: %v", call.ErrCannotJoinRoom, err)
	}

	sig, joinResp, err := lk.Connect(ctx, p.serverURL, token, p.allowInsecure, logf)
	if err != nil {
		return fmt.Errorf("%w: signal connect: %v", call.ErrCannotJoinRoom, err)
	}
	defer sig.Close()
	logf("joined room %q as %q", roomID, joinResp.Participant.GetIdentity())

	trackID := uuid.NewString()

	// Tell the server about our track before sending the offer.
	if err := sig.Send(&livekit.SignalRequest{
		Message: &livekit.SignalRequest_AddTrack{
			AddTrack: &livekit.AddTrackRequest{
				Cid:    trackID,
				Name:   "camera",
				Type:   livekit.TrackType_VIDEO,
				Source: livekit.TrackSource_CAMERA,
				Width:  640,
				Height: 480,
				Layers: []*livekit.VideoLayer{{
					Quality: livekit.VideoQuality_HIGH,
					Width:   640,
					Height:  480,
					Bitrate: 128_000,
				}},
			},
		},
	}); err != nil {
		return fmt.Errorf("%w: AddTrackRequest: %v", call.ErrCannotJoinRoom, err)
	}

	pc, track, offerSDP, err := lk.StartPionPublisher(logf, p.clientIP, joinResp, trackID)
	if err != nil {
		return fmt.Errorf("%w: pion publisher: %v", call.ErrCannotJoinRoom, err)
	}

	done := make(chan struct{})
	var once sync.Once
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		logf("[pion] ConnectionState: %s", s)
		if s == webrtc.PeerConnectionStateFailed ||
			s == webrtc.PeerConnectionStateDisconnected ||
			s == webrtc.PeerConnectionStateClosed {
			once.Do(func() { close(done) })
		}
	})

	wireTrickle(ctx, pc, sig, livekit.SignalTarget_PUBLISHER, sig.TricklesPub, logf)

	// Send offer.
	if err := sig.Send(&livekit.SignalRequest{
		Message: &livekit.SignalRequest_Offer{
			Offer: &livekit.SessionDescription{Type: "offer", Sdp: offerSDP},
		},
	}); err != nil {
		return fmt.Errorf("%w: send offer: %v", call.ErrCannotJoinRoom, err)
	}

	// Wait for server's answer.
	var answer *livekit.SessionDescription
	select {
	case answer = <-sig.Answers:
	case <-time.After(answerTimeout):
		return fmt.Errorf("%w: timeout waiting for SDP answer", call.ErrCannotJoinRoom)
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.Sdp,
	}); err != nil {
		return fmt.Errorf("%w: SetRemoteDescription: %v", call.ErrCannotJoinRoom, err)
	}

	// Wait for track_published confirmation.
	select {
	case pub := <-sig.Published:
		logf("track published: SID=%s", pub.GetTrack().GetSid())
	case <-time.After(publishedTimeout):
		logf("track_published timeout, proceeding anyway")
	case <-ctx.Done():
		return ctx.Err()
	}

	go ivf.LoopIntoTrack(logf, track, p.ivfPaths)
	logf("streaming IVF into room")

	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}

func (p *Plugin) runViewer(ctx context.Context, logf func(string, ...any), roomID, userID string) error {
	token, err := lk.MakeToken(p.apiKey, p.apiSecret, roomID, userID, false, true)
	if err != nil {
		return fmt.Errorf("%w: make token: %v", call.ErrCannotJoinRoom, err)
	}

	sig, joinResp, err := lk.Connect(ctx, p.serverURL, token, p.allowInsecure, logf)
	if err != nil {
		return fmt.Errorf("%w: signal connect: %v", call.ErrCannotJoinRoom, err)
	}
	defer sig.Close()
	logf("joined room %q as %q; existing participants: %d",
		roomID, joinResp.Participant.GetIdentity(), len(joinResp.OtherParticipants))

	// Wait for the server's first subscriber offer.
	var serverOffer *livekit.SessionDescription
	select {
	case serverOffer = <-sig.Offers:
	case <-time.After(offerTimeout):
		return fmt.Errorf("%w: timeout waiting for subscriber offer (no publishers in room?)", call.ErrCannotJoinRoom)
	case <-ctx.Done():
		return ctx.Err()
	}

	pc, answerSDP, err := lk.StartPionSubscriber(logf, p.clientIP, joinResp, serverOffer.Sdp)
	if err != nil {
		return fmt.Errorf("%w: pion subscriber: %v", call.ErrCannotJoinRoom, err)
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logf("[pion] OnTrack: %s PT=%d", track.Codec().MimeType, track.Codec().PayloadType)
		cfg := &viewer.Config{
			PacketsPerSample:  viewerPacketsPerSample,
			VP9RTPPayloadType: int(track.Codec().PayloadType), // use negotiated PT
			TrackBufferSize:   viewerTrackBufferSize,
		}
		if _, err := p.viewerManager.SpawnViewer(track, receiver, userID, cfg, nil); err != nil {
			logf("[viewer] SpawnViewer: %v", err)
		}
	})

	done := make(chan struct{})
	var once sync.Once
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		logf("[pion] ConnectionState: %s", s)
		if s == webrtc.PeerConnectionStateFailed ||
			s == webrtc.PeerConnectionStateDisconnected ||
			s == webrtc.PeerConnectionStateClosed {
			once.Do(func() { close(done) })
		}
	})

	wireTrickle(ctx, pc, sig, livekit.SignalTarget_SUBSCRIBER, sig.TricklesSub, logf)

	// Send answer.
	if err := sig.Send(&livekit.SignalRequest{
		Message: &livekit.SignalRequest_Answer{
			Answer: &livekit.SessionDescription{Type: "answer", Sdp: answerSDP},
		},
	}); err != nil {
		return fmt.Errorf("%w: send answer: %v", call.ErrCannotJoinRoom, err)
	}

	// Handle renegotiation offers (new publishers joining after us).
	go func() {
		for {
			select {
			case offer, ok := <-sig.Offers:
				if !ok {
					return
				}
				logf("[renegotiation] new offer from server")
				if err := pc.SetRemoteDescription(webrtc.SessionDescription{
					Type: webrtc.SDPTypeOffer,
					SDP:  offer.Sdp,
				}); err != nil {
					logf("[renegotiation] SetRemoteDescription: %v", err)
					continue
				}
				reAnswer, err := pc.CreateAnswer(nil)
				if err != nil {
					logf("[renegotiation] CreateAnswer: %v", err)
					continue
				}
				if err := pc.SetLocalDescription(reAnswer); err != nil {
					logf("[renegotiation] SetLocalDescription: %v", err)
					continue
				}
				if err := sig.Send(&livekit.SignalRequest{
					Message: &livekit.SignalRequest_Answer{
						Answer: &livekit.SessionDescription{Type: "answer", Sdp: reAnswer.SDP},
					},
				}); err != nil {
					logf("[renegotiation] send answer: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	logf("subscribed, waiting for tracks")
	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}
