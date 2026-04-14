package livekit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	lkproto "github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench"
	"github.com/rtcbench/rtcbench/pkg/ivf"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/viewer"
	lkinternal "github.com/rtcbench/rtcbench/plugin/livekit/internal"
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
	svcConfig          rtcbench.SVCCameraConfig
	log                *log.Logger
	logRegistry        *log.Registry
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	pipeline           *rtcbench.StatsPipeline
	maxSubscriptions   int // 0 = unlimited

	subscriptionCount int32 // atomic
}

func NewPlugin() rtcbench.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, e rtcbench.PluginEnv) error {
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

	pcapDir, err := rtcbench.SetupPacketCaptureDir(e.Config().Spec.Conference.PacketCapture)
	if err != nil {
		return fmt.Errorf("livekit: %w", err)
	}
	p.packetCaptureDir = pcapDir

	cams, err := rtcbench.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("livekit: %w", err)
	}
	p.cameras = cams

	p.pipeline = rtcbench.NewStatsPipeline(e)

	return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	p.pipeline.Stop()
	return nil
}

func (p *Plugin) NewParticipant(ctx context.Context, cfg *rtcbench.UserConfig) (rtcbench.Participant, error) {
	return &participant{
		plugin: p,
		userID: cfg.UserID,
		role:   cfg.Role,
		log:    p.logRegistry.NewLogger("livekit", fmt.Sprintf("[%s][%s]", cfg.Role, cfg.UserID)),
	}, nil
}

type participant struct {
	mu               sync.Mutex
	plugin           *Plugin
	userID           string
	role             rtcbench.UserRole
	log              *log.Logger
	room             *lksdk.Room
	getTargetBitrate func() int
	publishedTrackID string
	publishCancel    context.CancelFunc
}

func (p *participant) JoinRoom(ctx context.Context, req *rtcbench.JoinRequest) error {
	if req == nil || req.RoomID == "" {
		return rtcbench.ErrMissingRoomID
	}

	p.mu.Lock()
	if p.room != nil {
		p.mu.Unlock()
		return rtcbench.ErrConnectionExists
	}
	p.mu.Unlock()

	var (
		room             *lksdk.Room
		getTargetBitrate func() int
		err              error
	)
	switch p.role {
	case rtcbench.Sender:
		room, getTargetBitrate, err = p.plugin.connectSenderRoom(ctx, p.log, req.RoomID, p.userID)
	case rtcbench.Viewer:
		room, err = p.plugin.joinViewer(ctx, p.log, req.RoomID, p.userID)
	default:
		return rtcbench.ErrUnsupportedRole
	}
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.room = room
	p.getTargetBitrate = getTargetBitrate
	p.mu.Unlock()
	return nil
}

func (p *participant) LeaveRoom(ctx context.Context, req *rtcbench.LeaveRequest) error {
	return p.Close()
}

func (p *participant) Close() error {
	p.mu.Lock()
	room := p.room
	publishedTrackID := p.publishedTrackID
	publishCancel := p.publishCancel
	p.room = nil
	p.publishedTrackID = ""
	p.publishCancel = nil
	p.mu.Unlock()
	if publishCancel != nil {
		publishCancel()
	}
	if room != nil && publishedTrackID != "" {
		_ = room.LocalParticipant.UnpublishTrack(publishedTrackID)
	}
	if room != nil {
		room.Disconnect()
	}
	return nil
}

func (p *participant) PublishVideo(ctx context.Context, req *rtcbench.PublishVideoRequest) error {
	p.mu.Lock()
	if p.role != rtcbench.Sender {
		p.mu.Unlock()
		return rtcbench.ErrUnsupportedCapability
	}
	if p.room == nil {
		p.mu.Unlock()
		return fmt.Errorf("%w: participant not joined", rtcbench.ErrCannotJoinRoom)
	}
	if p.publishedTrackID != "" {
		p.mu.Unlock()
		return nil
	}
	room := p.room
	getTargetBitrate := p.getTargetBitrate
	p.mu.Unlock()

	trackID, cancel, err := p.plugin.publishVideo(ctx, p.log, room, getTargetBitrate)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.room != room {
		if cancel != nil {
			cancel()
		}
		_ = room.LocalParticipant.UnpublishTrack(trackID)
		return fmt.Errorf("%w: participant closed during publish", rtcbench.ErrCannotJoinRoom)
	}
	p.publishedTrackID = trackID
	p.publishCancel = cancel
	return nil
}

func (p *participant) UnpublishVideo(ctx context.Context, req *rtcbench.UnpublishVideoRequest) error {
	p.mu.Lock()
	if p.role != rtcbench.Sender {
		p.mu.Unlock()
		return rtcbench.ErrUnsupportedCapability
	}
	room := p.room
	trackID := p.publishedTrackID
	cancel := p.publishCancel
	p.publishedTrackID = ""
	p.publishCancel = nil
	p.mu.Unlock()

	if room == nil || trackID == "" {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	return room.LocalParticipant.UnpublishTrack(trackID)
}

func (p *Plugin) connectSenderRoom(ctx context.Context, l *log.Logger, roomID, userID string) (*lksdk.Room, func() int, error) {
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
			return nil, nil, fmt.Errorf("%w: build interceptors: %v", rtcbench.ErrCannotJoinRoom, err)
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
		return nil, nil, fmt.Errorf("%w: connect to room: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("connected to room %s (SVC=%v, autoSubscribe=false)", roomID, svc.Enabled)
	return room, getTargetBitrate, nil
}

func (p *Plugin) publishVideo(ctx context.Context, l *log.Logger, room *lksdk.Room, getTargetBitrate func() int) (string, context.CancelFunc, error) {
	if p.cameras == nil {
		return "", nil, fmt.Errorf("%w: no IVF files configured for sender", rtcbench.ErrCannotJoinRoom)
	}

	const initialBitrateBps = 1_200_000
	svc := ivf.ResolveSVC(p.cameras.Paths(), p.svcConfig.Mode, p.svcConfig.SpatialLayers, p.svcConfig.TemporalLayers, initialBitrateBps)
	loopCtx, cancel := context.WithCancel(context.Background())

	if svc.Enabled {
		rtpTrack, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeVP9,
				ClockRate: 90000,
			},
			"video", "ivf",
		)
		if err != nil {
			cancel()
			return "", nil, fmt.Errorf("%w: create track: %v", rtcbench.ErrCannotJoinRoom, err)
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
		publication, err := room.LocalParticipant.PublishTrack(rtpTrack, pubOpts)
		if err != nil {
			cancel()
			return "", nil, fmt.Errorf("%w: publish track: %v", rtcbench.ErrCannotJoinRoom, err)
		}
		l.Infof("SVC config: %d spatial x %d temporal layers (GCC target=%d bps)", svc.Config.NumSpatialLayers, svc.Config.NumTemporalLayers, svc.Config.TargetBitrateBps)
		go ivf.SVCLoopIntoTrackUntil(loopCtx, l, rtpTrack, p.cameras.NewSource(), svc.Config, getTargetBitrate)
		return publication.SID(), cancel, nil
	} else {
		sampleTrack, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeVP9,
				ClockRate: 90000,
			},
			"video", "ivf",
		)
		if err != nil {
			cancel()
			return "", nil, fmt.Errorf("%w: create track: %v", rtcbench.ErrCannotJoinRoom, err)
		}
		publication, err := room.LocalParticipant.PublishTrack(sampleTrack, &lksdk.TrackPublicationOptions{Name: "video"})
		if err != nil {
			cancel()
			return "", nil, fmt.Errorf("%w: publish track: %v", rtcbench.ErrCannotJoinRoom, err)
		}
		go ivf.LoopIntoTrackUntil(loopCtx, l, sampleTrack, p.cameras.NewSource())
		return publication.SID(), cancel, nil
	}
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

func (p *Plugin) joinViewer(ctx context.Context, l *log.Logger, roomID, userID string) (*lksdk.Room, error) {
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
		return nil, fmt.Errorf("%w: connect to room: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("connected to room %s as viewer", roomID)
	return room, nil
}
