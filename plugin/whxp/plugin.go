package whxp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
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
	whxp "call.zip/plugin/whxp/internal"
	"github.com/pion/webrtc/v3"
)

const (
	PluginID = "whxp"

	cfgWhipURL        = "whipURL"
	cfgWhepURL        = "whepURL"
	cfgBearerToken    = "bearerToken"
	cfgAllowInsecure  = "allowInsecureHttps"

	viewerPacketsPerSample = 1000
	viewerTrackBufferSize  = 1500
	statsInputChanSize     = 1 << 16 // 65536
)

type session struct {
	pc          *webrtc.PeerConnection
	resourceURL string
}

type Plugin struct {
	client             *whxp.Client
	whipURL            string
	whepURL            string
	log                *log.Logger
	logRegistry        *log.Registry
	clientIP           string
	cameras            *ivf.Cameras
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	viewerManager      *viewer.Manager
	publisher          *vp9_stats.Publisher

	mu       sync.Mutex
	sessions []session
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, e call.PluginEnv) error {
	cfg := e.Config().Spec.PluginConfig[PluginID].(map[string]any)

	if v, ok := cfg[cfgWhipURL]; ok {
		p.whipURL = v.(string)
	}
	if v, ok := cfg[cfgWhepURL]; ok {
		p.whepURL = v.(string)
	}

	var bearerToken string
	if v, ok := cfg[cfgBearerToken]; ok {
		bearerToken = v.(string)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if v, ok := cfg[cfgAllowInsecure]; ok && v.(bool) {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient := &http.Client{Transport: transport}
	p.client = whxp.NewClient(httpClient, bearerToken)

	p.log = e.LogRegistry().NewLogger("whxp", "")
	p.logRegistry = e.LogRegistry()
	p.clientIP = e.Config().Spec.Network.ClientIP
	p.enableRecording = e.Config().Spec.Conference.Recording.Enabled
	p.recordingDirectory = e.Config().Spec.Conference.Recording.Directory
	p.statsBufferSize = e.Config().Spec.Conference.StatsBufferSize

	if e.Config().Spec.Conference.PacketCapture.Enabled {
		ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
		p.packetCaptureDir = filepath.Join(e.Config().Spec.Conference.PacketCapture.Directory, ts)
		if err := os.MkdirAll(p.packetCaptureDir, 0o755); err != nil {
			return fmt.Errorf("whxp: packet capture directory: %w", err)
		}
	}

	if e.Config().Spec.Conference.Cameras.PerRoom > 0 {
		cams, err := ivf.NewCameras(e.Config().Spec.Conference.Cameras.Directory, e.Config().Spec.Conference.Cameras.InMemory)
		if err != nil {
			return fmt.Errorf("whxp: %w", err)
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
	sessions := p.sessions
	p.sessions = nil
	p.mu.Unlock()

	for _, s := range sessions {
		s.pc.Close()
		p.client.Delete(s.resourceURL)
	}

	p.viewerManager.StopAll()
	p.publisher.Stop()
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	l := p.logRegistry.NewLogger("whxp", fmt.Sprintf("[%s][%s]", role, userID))

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
	if p.whipURL == "" {
		return fmt.Errorf("%w: whipURL not configured", call.ErrCannotJoinRoom)
	}

	pc, track, offerSDP, err := whxp.StartPublisher(l, p.clientIP)
	if err != nil {
		return fmt.Errorf("%w: pion publisher: %v", call.ErrCannotJoinRoom, err)
	}

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
	})

	answerSDP, resourceURL, err := p.client.WHIP(p.whipURL, offerSDP)
	if err != nil {
		pc.Close()
		return fmt.Errorf("%w: WHIP: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("WHIP negotiation complete, resource=%s", resourceURL)

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		pc.Close()
		return fmt.Errorf("%w: SetRemoteDescription: %v", call.ErrCannotJoinRoom, err)
	}

	go ivf.LoopIntoTrack(l, track, p.cameras.NewSource())

	p.mu.Lock()
	p.sessions = append(p.sessions, session{pc: pc, resourceURL: resourceURL})
	p.mu.Unlock()

	return nil
}

func (p *Plugin) runViewer(ctx context.Context, l *log.Logger, roomID, userID string) error {
	if p.whepURL == "" {
		return fmt.Errorf("%w: whepURL not configured", call.ErrCannotJoinRoom)
	}

	pc, offerSDP, err := whxp.StartSubscriber(l, p.clientIP)
	if err != nil {
		return fmt.Errorf("%w: pion subscriber: %v", call.ErrCannotJoinRoom, err)
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
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
		if _, err := p.viewerManager.SpawnViewer(track, receiver, userID, cfg, seg); err != nil {
			l.Errorf("[viewer] SpawnViewer failed: %v", err)
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
	})

	answerSDP, resourceURL, err := p.client.WHEP(p.whepURL, offerSDP)
	if err != nil {
		pc.Close()
		return fmt.Errorf("%w: WHEP: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("WHEP negotiation complete, resource=%s", resourceURL)

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		pc.Close()
		return fmt.Errorf("%w: SetRemoteDescription: %v", call.ErrCannotJoinRoom, err)
	}

	p.mu.Lock()
	p.sessions = append(p.sessions, session{pc: pc, resourceURL: resourceURL})
	p.mu.Unlock()

	return nil
}
