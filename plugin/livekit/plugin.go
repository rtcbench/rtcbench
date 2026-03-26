package livekit

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/log"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
	lkinternal "call.zip/plugin/livekit/internal"
	lkproto "github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v3"
)

const (
	PluginID = "livekit"

	cfgWSURL     = "wsURL"
	cfgAPIKey    = "apiKey"
	cfgAPISecret = "apiSecret"

	viewerPacketsPerSample = 200
	viewerTrackBufferSize  = 1500

	// PLI rate limiting: at most one PLI per 100ms
	pliMinIntervalNanos = 100_000_000
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
	viewerManager      *viewer.Manager
	publisher          *vp9_stats.Publisher

	mu    sync.Mutex
	rooms []*lksdk.Room
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, e call.PluginEnv) error {
	cfg := e.Config().Spec.PluginConfig[PluginID].(map[string]any)
	p.wsURL = cfg[cfgWSURL].(string)
	p.apiKey = cfg[cfgAPIKey].(string)
	p.apiSecret = cfg[cfgAPISecret].(string)

	p.log = e.LogRegistry().NewLogger("livekit", "")
	p.logRegistry = e.LogRegistry()
	p.enableRecording = e.Config().Spec.Conference.Recording.Enabled
	p.recordingDirectory = e.Config().Spec.Conference.Recording.Directory
	p.statsBufferSize = e.Config().Spec.Conference.StatsBufferSize
	p.svcConfig = e.Config().Spec.Conference.Cameras.SVC

	if e.Config().Spec.Conference.PacketCapture.Enabled {
		ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
		p.packetCaptureDir = filepath.Join(e.Config().Spec.Conference.PacketCapture.Directory, ts)
		if err := os.MkdirAll(p.packetCaptureDir, 0o755); err != nil {
			return fmt.Errorf("livekit: packet capture directory: %w", err)
		}
	}

	if e.Config().Spec.Conference.Cameras.PerRoom > 0 {
		cams, err := ivf.NewCameras(e.Config().Spec.Conference.Cameras.Directory, e.Config().Spec.Conference.Cameras.InMemory)
		if err != nil {
			return fmt.Errorf("livekit: %w", err)
		}
		p.cameras = cams
	}

	statsInput := make(chan vp9_stats.VideoQualitySample, e.Config().Spec.Conference.StatsInputChanSize)
	statsLog := e.LogRegistry().NewLogger("video_stats", "")
	p.viewerManager = viewer.NewManager(statsInput, statsLog)
	p.publisher = vp9_stats.NewPublisher(statsInput)
	p.publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		statsLog.Infof("bitrate=%s,period=%s,sample=%s", sample.Mbps(), period.String(), sample.String())
	})
	for _, consumer := range e.StatsConsumers() {
		p.publisher.AddSubscriber(consumer)
	}
	go p.publisher.Run()

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

	p.viewerManager.StopAll()
	p.publisher.Stop()
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

	// Build interceptor chain: SDK defaults + GCC bandwidth estimation.
	// The twccExt interceptor injects transport-cc sequence numbers into
	// outgoing RTP packets so GCC can track them.
	// 1.2 Mbps matches the target total for 3-layer SVC (S0=150K+S1=350K+S2=700K).
	// S2 threshold = 71% × 1.2M = 852 Kbps, so call.zip stays at 1080p through
	// the 0.8 Mbps BW cap phase, only dropping to lower layers at 0.5/0.3 Mbps.
	const initialBitrateBps = 1_200_000
	interceptors, getTargetBitrate, err := lkinternal.SenderInterceptors(initialBitrateBps)
	if err != nil {
		return fmt.Errorf("%w: build interceptors: %v", call.ErrCannotJoinRoom, err)
	}

	room, err := lksdk.ConnectToRoom(p.wsURL, lksdk.ConnectInfo{
		APIKey:              p.apiKey,
		APISecret:           p.apiSecret,
		RoomName:            roomID,
		ParticipantIdentity: userID,
	}, &lksdk.RoomCallback{}, lksdk.WithInterceptors(interceptors), lksdk.WithAutoSubscribe(false))
	if err != nil {
		return fmt.Errorf("%w: connect to room: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("connected to room %s (GCC enabled, initial=%d bps, autoSubscribe=false)", roomID, initialBitrateBps)

	p.mu.Lock()
	p.rooms = append(p.rooms, room)
	p.mu.Unlock()

	svc := ivf.ResolveSVC(p.cameras.Paths(), p.svcConfig.Mode, p.svcConfig.SpatialLayers, p.svcConfig.TemporalLayers, initialBitrateBps)

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
	room, err := lksdk.ConnectToRoom(p.wsURL, lksdk.ConnectInfo{
		APIKey:              p.apiKey,
		APISecret:           p.apiSecret,
		RoomName:            roomID,
		ParticipantIdentity: userID,
	}, &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
				l.Infof("[pion] OnTrack: %s %s PT=%d", track.Kind(), track.Codec().MimeType, track.PayloadType())
				// Request highest quality so the SFU forwards the top spatial layer.
				if err := pub.SetVideoQuality(lkproto.VideoQuality_HIGH); err != nil {
					l.Errorf("[pion] SetVideoQuality: %v", err)
				}
				cfg := &viewer.Config{
					PacketsPerSample:  viewerPacketsPerSample,
					VP9RTPPayloadType: int(track.PayloadType()),
					TrackBufferSize:   viewerTrackBufferSize,
					StatsBufferSize:   p.statsBufferSize,
					PacketCaptureDir:  p.packetCaptureDir,
				}
				var seg *vp9.IvfSegmenter
				if p.enableRecording {
					recDir := path.Join(p.recordingDirectory, fmt.Sprintf("/room=%s/user=%s", roomID, userID))
					var recErr error
					seg, recErr = vp9.NewIvfSegmenter(recDir)
					if recErr != nil {
						l.Errorf("[viewer] IvfSegmenter failed: %v", recErr)
					} else {
						seg.Enable()
						l.Infof("enabled IVF file writing for room=%s user=%s", roomID, userID)
					}
				}

				// Set up RTCP feedback: PLI on frame loss
				rtcpTracker := &vp9_stats.RTCPTracker{}
				ssrc := track.SSRC()
				sendPLI := func() {
					rp.WritePLI(ssrc)
					l.Infof("[rtcp] sent PLI ssrc=%d", ssrc)
				}
				throttle := vp9_stats.NewPLIThrottle(sendPLI, rtcpTracker, pliMinIntervalNanos)

				if _, err := p.viewerManager.SpawnViewer(track, nil, userID, cfg, seg, p.publisher, rtcpTracker, throttle.OnFrameLost); err != nil {
					l.Errorf("[viewer] SpawnViewer failed: %v", err)
				}
			},
		},
	})
	if err != nil {
		return fmt.Errorf("%w: connect to room: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("connected to room %s as viewer", roomID)

	p.mu.Lock()
	p.rooms = append(p.rooms, room)
	p.mu.Unlock()

	return nil
}
