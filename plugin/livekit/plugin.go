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
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v3"
)

const (
	PluginID = "livekit"

	cfgWSURL     = "wsURL"
	cfgAPIKey    = "apiKey"
	cfgAPISecret = "apiSecret"

	viewerPacketsPerSample = 1000
	viewerTrackBufferSize  = 1500
	statsInputChanSize     = 1 << 16 // 65536 - large enough for high viewer counts
)

type Plugin struct {
	wsURL              string
	apiKey             string
	apiSecret          string
	cameras            *ivf.Cameras
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

	statsInput := make(chan vp9_stats.VideoQualitySample, statsInputChanSize)
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

	room, err := lksdk.ConnectToRoom(p.wsURL, lksdk.ConnectInfo{
		APIKey:              p.apiKey,
		APISecret:           p.apiSecret,
		RoomName:            roomID,
		ParticipantIdentity: userID,
	}, &lksdk.RoomCallback{})
	if err != nil {
		return fmt.Errorf("%w: connect to room: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("connected to room %s", roomID)

	p.mu.Lock()
	p.rooms = append(p.rooms, room)
	p.mu.Unlock()

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP9,
			ClockRate: 90000,
		},
		"video", "ivf",
	)
	if err != nil {
		return fmt.Errorf("%w: create track: %v", call.ErrCannotJoinRoom, err)
	}

	_, err = room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
		Name: "video",
	})
	if err != nil {
		return fmt.Errorf("%w: publish track: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("published VP9 track")

	go ivf.LoopIntoTrack(l, track, p.cameras.NewSource())

	return nil
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
				if _, err := p.viewerManager.SpawnViewer(track, nil, userID, cfg, seg); err != nil {
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
