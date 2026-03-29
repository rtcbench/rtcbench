package livekit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/log"
	"call.zip/pkg/viewer"
	lkinternal "call.zip/plugin/livekit/internal"
	lkproto "github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
)

const (
	PluginID = "livekit"

	cfgWSURL            = "wsURL"
	cfgAPIKey           = "apiKey"
	cfgAPISecret        = "apiSecret"
	cfgMaxSubscriptions = "maxSubscriptions"

	viewerPacketsPerSample = 200
)

type Plugin struct {
	wsURL              string
	apiKey             string
	apiSecret          string
	cameras            *ivf.Cameras
	svcConfig          call.SVCCameraConfig
	log                *log.Logger
	logRegistry        *log.Registry
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	pipeline           *call.StatsPipeline
	maxSubscriptions   int // 0 = unlimited

	mu                sync.Mutex
	rooms             []*lksdk.Room
	subscriptionCount int32 // atomic
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, e call.PluginEnv) error {
	cfg := e.Config().Spec.PluginConfig[PluginID].(map[string]any)
	p.wsURL = cfg[cfgWSURL].(string)
	p.apiKey = cfg[cfgAPIKey].(string)
	p.apiSecret = cfg[cfgAPISecret].(string)
	if ms, ok := cfg[cfgMaxSubscriptions]; ok {
		switch v := ms.(type) {
		case int:
			p.maxSubscriptions = v
		case int64:
			p.maxSubscriptions = int(v)
		case uint64:
			p.maxSubscriptions = int(v)
		case float64:
			p.maxSubscriptions = int(v)
		}
	}

	p.log = e.LogRegistry().NewLogger("livekit", "")
	p.logRegistry = e.LogRegistry()
	p.enableRecording = e.Config().Spec.Conference.Recording.Enabled
	p.recordingDirectory = e.Config().Spec.Conference.Recording.Directory
	p.statsBufferSize = e.Config().Spec.Conference.StatsBufferSize
	p.svcConfig = e.Config().Spec.Conference.Cameras.SVC

	pcapDir, err := call.SetupPacketCaptureDir(e.Config().Spec.Conference.PacketCapture)
	if err != nil {
		return fmt.Errorf("livekit: %w", err)
	}
	p.packetCaptureDir = pcapDir

	cams, err := call.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("livekit: %w", err)
	}
	p.cameras = cams

	p.pipeline = call.NewStatsPipeline(e)

	return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	rooms := p.rooms
	p.rooms = nil
	p.mu.Unlock()

	for _, room := range rooms {
		room.Disconnect()
	}

	p.pipeline.Stop()
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	l := p.logRegistry.NewLogger("livekit", fmt.Sprintf("[%s][%s]", role, userID))

	switch role {
	case call.Sender:
		return p.runSender(ctx, l, roomID, userID)
	case call.Viewer:
		return p.runViewer(ctx, l, roomID, userID)
	default:
		return call.ErrUnsupportedRole
	}
}

func (p *Plugin) runSender(ctx context.Context, l *log.Logger, roomID, userID string) error {
	if p.cameras == nil {
		return fmt.Errorf("%w: no IVF files configured for sender", call.ErrCannotJoinRoom)
	}

	// Resolve SVC before connecting so we know whether to add GCC interceptors.
	// When SVC is off (e.g. during benchmarks) we skip GCC entirely — no TWCC
	// header injection, no bandwidth estimation, no adaptive bitrate overhead.
	const initialBitrateBps = 1_200_000
	svc := ivf.ResolveSVC(p.cameras.Paths(), p.svcConfig.Mode, p.svcConfig.SpatialLayers, p.svcConfig.TemporalLayers, initialBitrateBps)

	connectOpts := []lksdk.ConnectOption{lksdk.WithAutoSubscribe(false)}
	var getTargetBitrate func() int

	if svc.Enabled {
		// Build interceptor chain: SDK defaults + GCC bandwidth estimation.
		// 1.2 Mbps matches the target total for 3-layer SVC (S0=150K+S1=350K+S2=700K).
		interceptors, getBitrate, err := lkinternal.SenderInterceptors(initialBitrateBps)
		if err != nil {
			return fmt.Errorf("%w: build interceptors: %v", call.ErrCannotJoinRoom, err)
		}
		getTargetBitrate = getBitrate
		connectOpts = append(connectOpts, lksdk.WithInterceptors(interceptors))
	}

	room, err := lksdk.ConnectToRoom(p.wsURL, lksdk.ConnectInfo{
		APIKey:              p.apiKey,
		APISecret:           p.apiSecret,
		RoomName:            roomID,
		ParticipantIdentity: userID,
	}, &lksdk.RoomCallback{}, connectOpts...)
	if err != nil {
		return fmt.Errorf("%w: connect to room: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("connected to room %s (SVC=%v, autoSubscribe=false)", roomID, svc.Enabled)

	p.mu.Lock()
	p.rooms = append(p.rooms, room)
	p.mu.Unlock()

	if svc.Enabled {
		rtpTrack, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeVP9,
				ClockRate: 90000,
			},
			"video", "ivf",
		)
		if err != nil {
			return fmt.Errorf("%w: create track: %v", call.ErrCannotJoinRoom, err)
		}
		// Declare all SVC spatial layers to the SFU so it forwards them correctly.
		// Without this the SFU treats the track as single-layer and strips S1/S2.
		svcLayers := buildSVCLayers(svc.Config)
		pubOpts := &lksdk.TrackPublicationOptions{
			Name:        "video",
			VideoWidth:  int(svc.Config.Widths[svc.Config.NumSpatialLayers-1]),
			VideoHeight: int(svc.Config.Heights[svc.Config.NumSpatialLayers-1]),
			VideoLayers: svcLayers,
		}
		if _, err = room.LocalParticipant.PublishTrack(rtpTrack, pubOpts); err != nil {
			return fmt.Errorf("%w: publish track: %v", call.ErrCannotJoinRoom, err)
		}
		l.Infof("SVC config: %d spatial x %d temporal layers (GCC target=%d bps)", svc.Config.NumSpatialLayers, svc.Config.NumTemporalLayers, svc.Config.TargetBitrateBps)
		go ivf.SVCLoopIntoTrack(l, rtpTrack, p.cameras.NewSource(), svc.Config, getTargetBitrate)
	} else {
		sampleTrack, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeVP9,
				ClockRate: 90000,
			},
			"video", "ivf",
		)
		if err != nil {
			return fmt.Errorf("%w: create track: %v", call.ErrCannotJoinRoom, err)
		}
		if _, err = room.LocalParticipant.PublishTrack(sampleTrack, &lksdk.TrackPublicationOptions{Name: "video"}); err != nil {
			return fmt.Errorf("%w: publish track: %v", call.ErrCannotJoinRoom, err)
		}
		go ivf.LoopIntoTrack(l, sampleTrack, p.cameras.NewSource())
	}

	return nil
}

// buildSVCLayers converts an SVCConfig into the VideoLayer slice needed by the
// LiveKit SFU to recognise this track as multi-spatial-layer VP9 SVC.
// Without these layers the SFU treats the track as single-layer and only
// forwards S0 packets, ignoring the SID field in the VP9 payload descriptor.
func buildSVCLayers(cfg ivf.SVCConfig) []*lkproto.VideoLayer {
	qualities := []lkproto.VideoQuality{
		lkproto.VideoQuality_LOW,
		lkproto.VideoQuality_MEDIUM,
		lkproto.VideoQuality_HIGH,
	}
	// Spatial bitrate fractions (same as layerBitrateThreshold: 6%, 23%, 71%)
	spatialFrac := []float64{0.06, 0.23, 0.71}
	layers := make([]*lkproto.VideoLayer, cfg.NumSpatialLayers)
	for i := range cfg.NumSpatialLayers {
		qi := i + (3 - cfg.NumSpatialLayers) // right-align to LOW/MED/HIGH
		var bps uint32
		if cfg.TargetBitrateBps > 0 {
			bps = uint32(float64(cfg.TargetBitrateBps) * spatialFrac[qi])
		}
		layers[i] = &lkproto.VideoLayer{
			Quality: qualities[qi],
			Width:   uint32(cfg.Widths[i]),
			Height:  uint32(cfg.Heights[i]),
			Bitrate: bps,
		}
	}
	return layers
}

func (p *Plugin) runViewer(ctx context.Context, l *log.Logger, roomID, userID string) error {
	onTrackSubscribed := func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
		l.Infof("[pion] OnTrack: %s %s PT=%d", track.Kind(), track.Codec().MimeType, track.PayloadType())
		// Request highest quality so the SFU forwards the top spatial layer.
		if err := pub.SetVideoQuality(lkproto.VideoQuality_HIGH); err != nil {
			l.Errorf("[pion] SetVideoQuality: %v", err)
		}
		ssrc := track.SSRC()
		p.pipeline.ViewerManager.HandleTrack(track, nil, p.pipeline.Publisher, viewer.TrackHandlerOpts{
			StatsBufferSize:    p.statsBufferSize,
			PacketCaptureDir:   p.packetCaptureDir,
			EnableRecording:    p.enableRecording,
			RecordingDirectory: p.recordingDirectory,
			PacketsPerSample:   viewerPacketsPerSample,
			RoomID:             roomID,
			UserID:             userID,
			Logger:             l,
			SendPLI: func() {
				rp.WritePLI(ssrc)
				l.Infof("[rtcp] sent PLI ssrc=%d", ssrc)
			},
		})
	}

	cb := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: onTrackSubscribed,
		},
	}

	var opts []lksdk.ConnectOption
	if p.maxSubscriptions > 0 {
		// Limit subscriptions: subscribe to first maxSubscriptions video tracks only.
		opts = append(opts, lksdk.WithAutoSubscribe(false))
		cb.ParticipantCallback.OnTrackPublished = func(pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
			if pub.Kind() != lksdk.TrackKindVideo {
				return
			}
			n := atomic.AddInt32(&p.subscriptionCount, 1)
			if int(n) > p.maxSubscriptions {
				atomic.AddInt32(&p.subscriptionCount, -1)
				return
			}
			if err := pub.SetSubscribed(true); err != nil {
				l.Errorf("[viewer] SetSubscribed failed: %v", err)
				atomic.AddInt32(&p.subscriptionCount, -1)
			}
		}
		l.Infof("viewer: maxSubscriptions=%d (auto-subscribe disabled)", p.maxSubscriptions)
	}

	room, err := lksdk.ConnectToRoom(p.wsURL, lksdk.ConnectInfo{
		APIKey:              p.apiKey,
		APISecret:           p.apiSecret,
		RoomName:            roomID,
		ParticipantIdentity: userID,
	}, cb, opts...)
	if err != nil {
		return fmt.Errorf("%w: connect to room: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("connected to room %s as viewer", roomID)

	p.mu.Lock()
	p.rooms = append(p.rooms, room)
	p.mu.Unlock()

	return nil
}
