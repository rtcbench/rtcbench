package livekit

import (
	"context"
	"fmt"
	"log"
	"path"
	"sync"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/pion/webrtc/v3"
)

const (
	PluginID = "livekit"

	cfgWSURL     = "wsURL"
	cfgAPIKey    = "apiKey"
	cfgAPISecret = "apiSecret"

	viewerPacketsPerSample = 1000
	viewerTrackBufferSize  = 1500
	statsInputChanSize     = 128
)

type Plugin struct {
	wsURL              string
	apiKey             string
	apiSecret          string
	cameras            *ivf.Cameras
	enableRecording    bool
	recordingDirectory string
	statsBufferSize    int
	viewerManager      *viewer.Manager
	publisher          *vp9_stats.Publisher

	mu    sync.Mutex
	rooms []*lksdk.Room
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, config *call.Config) error {
	cfg := config.Spec.PluginConfig[PluginID].(map[string]any)
	p.wsURL = cfg[cfgWSURL].(string)
	p.apiKey = cfg[cfgAPIKey].(string)
	p.apiSecret = cfg[cfgAPISecret].(string)

	p.enableRecording = config.Spec.Conference.Recording.Enabled
	p.recordingDirectory = config.Spec.Conference.Recording.Directory
	p.statsBufferSize = config.Spec.Conference.StatsBufferSize

	if config.Spec.Conference.Cameras.PerRoom > 0 {
		cams, err := ivf.NewCameras(config.Spec.Conference.Cameras.Directory, config.Spec.Conference.Cameras.InMemory)
		if err != nil {
			return fmt.Errorf("livekit: %w", err)
		}
		p.cameras = cams
	}

	statsInput := make(chan vp9_stats.VideoQualitySample, statsInputChanSize)
	p.viewerManager = viewer.NewManager(statsInput)
	p.publisher = vp9_stats.NewPublisher(statsInput)
	p.publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		log.Printf("[sub-data-v1] bitrate=%s,period=%s,sample=%s", sample.Mbps(), period.String(), sample.String())
	})
	for _, consumer := range config.StatsConsumers {
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

func (p *Plugin) runSender(ctx context.Context, logf func(string, ...any), roomID, userID string) error {
	if p.cameras == nil {
		return fmt.Errorf("%w: no IVF files configured for sender", call.ErrCannotJoinRoom)
	}

	done := make(chan struct{})
	var once sync.Once

	room, err := lksdk.ConnectToRoom(p.wsURL, lksdk.ConnectInfo{
		APIKey:              p.apiKey,
		APISecret:           p.apiSecret,
		RoomName:            roomID,
		ParticipantIdentity: userID,
	}, &lksdk.RoomCallback{
		OnDisconnected: func() {
			once.Do(func() { close(done) })
		},
	})
	if err != nil {
		return fmt.Errorf("%w: connect to room: %v", call.ErrCannotJoinRoom, err)
	}
	logf("connected to room %s", roomID)

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
	logf("published VP9 track")

	go ivf.LoopIntoTrack(logf, track, p.cameras.NewSource())

	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}

func (p *Plugin) runViewer(ctx context.Context, logf func(string, ...any), roomID, userID string) error {
	done := make(chan struct{})
	var once sync.Once

	room, err := lksdk.ConnectToRoom(p.wsURL, lksdk.ConnectInfo{
		APIKey:              p.apiKey,
		APISecret:           p.apiSecret,
		RoomName:            roomID,
		ParticipantIdentity: userID,
	}, &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
				logf("[pion] OnTrack: %s %s PT=%d", track.Kind(), track.Codec().MimeType, track.PayloadType())
				cfg := &viewer.Config{
					PacketsPerSample:  viewerPacketsPerSample,
					VP9RTPPayloadType: int(track.PayloadType()),
					TrackBufferSize:   viewerTrackBufferSize,
					StatsBufferSize:   p.statsBufferSize,
				}
				var seg *vp9.IvfSegmenter
				if p.enableRecording {
					recDir := path.Join(p.recordingDirectory, fmt.Sprintf("/room=%s/user=%s", roomID, userID))
					var recErr error
					seg, recErr = vp9.NewIvfSegmenter(recDir)
					if recErr != nil {
						logf("[viewer] IvfSegmenter failed: %v", recErr)
					} else {
						seg.Enable()
						log.Printf("Enabled IVF file writing for room=%s user=%s", roomID, userID)
					}
				}
				if _, err := p.viewerManager.SpawnViewer(track, nil, userID, cfg, seg); err != nil {
					logf("[viewer] SpawnViewer failed: %v", err)
				}
			},
		},
		OnDisconnected: func() {
			once.Do(func() { close(done) })
		},
	})
	if err != nil {
		return fmt.Errorf("%w: connect to room: %v", call.ErrCannotJoinRoom, err)
	}
	logf("connected to room %s as viewer", roomID)

	p.mu.Lock()
	p.rooms = append(p.rooms, room)
	p.mu.Unlock()

	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}
