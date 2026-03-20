package janus

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/log"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
	janus "call.zip/plugin/janus/internal"
	"github.com/pion/webrtc/v3"
)

const (
	PluginID = "janus"

	cfgServerRoot    = "serverRoot"
	cfgAllowInsecure = "allowInsecureHttps"

	videoroomPlugin = "janus.plugin.videoroom"

	viewerPacketsPerSample = 1000
	viewerTrackBufferSize  = 1500
	statsInputChanSize     = 1 << 16 // 65536 - large enough for high viewer counts
)

type Plugin struct {
	client             *janus.Client
	log                *log.Logger
	logRegistry        *log.Registry
	serverIP           string
	clientIP           string
	cameras            *ivf.Cameras
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	viewerManager      *viewer.Manager
	publisher          *vp9_stats.Publisher
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, config *call.Config) error {
	baseURL := config.Spec.PluginConfig[PluginID].(map[string]any)[cfgServerRoot].(string)
	allowInsecure := config.Spec.PluginConfig[PluginID].(map[string]any)[cfgAllowInsecure].(bool)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if allowInsecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient := &http.Client{Transport: transport}

	p.client = janus.NewClient(httpClient, baseURL)
	p.log = config.Log.NewLogger("janus", "")
	p.logRegistry = config.Log

	info, err := p.client.GetInfo()
	if err != nil {
		return err
	}
	p.log.Infof("found Janus server: %q; version: %q", info["name"], info["version_string"])

	p.serverIP = config.Spec.Network.ServerIP
	p.clientIP = config.Spec.Network.ClientIP
	p.enableRecording = config.Spec.Conference.Recording.Enabled
	p.recordingDirectory = config.Spec.Conference.Recording.Directory
	p.statsBufferSize = config.Spec.Conference.StatsBufferSize

	if config.Spec.Conference.PacketCapture.Enabled {
		ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
		p.packetCaptureDir = filepath.Join(config.Spec.Conference.PacketCapture.Directory, ts)
		if err := os.MkdirAll(p.packetCaptureDir, 0o755); err != nil {
			return fmt.Errorf("janus: packet capture directory: %w", err)
		}
	}

	// Validate that the conference name contains a parseable integer for the Janus room ID.
	if _, err := roomIDFromName(config.Spec.Conference.Name); err != nil {
		return err
	}

	// Load IVF camera files for senders.
	if config.Spec.Conference.Cameras.PerRoom > 0 {
		cams, err := ivf.NewCameras(config.Spec.Conference.Cameras.Directory, config.Spec.Conference.Cameras.InMemory)
		if err != nil {
			return fmt.Errorf("janus: %w", err)
		}
		p.cameras = cams
	}

	// Stats pipeline shared across all viewer goroutines.
	statsInput := make(chan vp9_stats.VideoQualitySample, statsInputChanSize)
	statsLog := config.Log.NewLogger("video_stats", "")
	p.viewerManager = viewer.NewManager(statsInput, statsLog)
	p.publisher = vp9_stats.NewPublisher(statsInput)
	p.publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		statsLog.Infof("bitrate=%s,period=%s,sample=%s", sample.Mbps(), period.String(), sample.String())
	})
	for _, consumer := range config.StatsConsumers {
		p.publisher.AddSubscriber(consumer)
	}
	go p.publisher.Run()

	return nil
}

// roomIDFromName parses the trailing digits of name as the Janus room ID.
// "room-1234" → 1234, "42" → 42, "42abc" → error, "nodigits" → error.
func roomIDFromName(name string) (int64, error) {
	end := len(name)
	if end == 0 || name[end-1] < '0' || name[end-1] > '9' {
		return 0, fmt.Errorf("janus: conference name %q must end with digits for room ID", name)
	}
	start := end - 1
	for start > 0 && name[start-1] >= '0' && name[start-1] <= '9' {
		start--
	}
	id, err := strconv.ParseInt(name[start:end], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("janus: could not parse room ID from %q: %w", name, err)
	}
	return id, nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	p.viewerManager.StopAll()
	p.publisher.Stop()
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	roomInt, err := roomIDFromName(roomID)
	if err != nil {
		return err
	}

	l := p.logRegistry.NewLogger("janus", fmt.Sprintf("[%s][%s]", role, userID))

	switch role {
	case call.Sender:
		return p.runSender(ctx, l, roomInt, userID)
	case call.Viewer:
		return p.runViewer(ctx, l, roomInt, userID)
	default:
		return call.ErrUnsupportedRole
	}
}

func (p *Plugin) runSender(ctx context.Context, l *log.Logger, roomID int64, userID string) error {
	if p.cameras == nil {
		return fmt.Errorf("%w: no IVF files configured for sender", call.ErrCannotJoinRoom)
	}

	sessionID, err := p.client.CreateSession()
	if err != nil {
		return fmt.Errorf("%w: create session: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("session created: %d", sessionID)

	session := janus.NewSession(ctx, p.client, sessionID, l)
	defer session.Close()

	handleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		return fmt.Errorf("%w: attach plugin: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("handle attached: %d", handleID)

	joined, err := session.Send(handleID, map[string]any{
		"request": "join",
		"ptype":   "publisher",
		"room":    roomID,
		"display": userID,
	}, nil)
	if err != nil {
		return fmt.Errorf("%w: join: %v", call.ErrCannotJoinRoom, err)
	}
	if joined.PluginData == nil || joined.PluginData.Data["videoroom"] != "joined" {
		return fmt.Errorf("%w: unexpected join response: %v", call.ErrCannotJoinRoom, joined)
	}
	l.Infof("joined room %d as publisher", roomID)

	pc, track, offerSDP, err := janus.StartPionPublisher(l, p.clientIP)
	if err != nil {
		return fmt.Errorf("%w: pion publisher: %v", call.ErrCannotJoinRoom, err)
	}

	done := make(chan struct{})
	var once sync.Once
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
		if s == webrtc.PeerConnectionStateFailed ||
			s == webrtc.PeerConnectionStateDisconnected ||
			s == webrtc.PeerConnectionStateClosed {
			once.Do(func() { close(done) })
		}
	})

	configured, err := session.Send(handleID, map[string]any{
		"request":    "publish",
		"videocodec": "vp9",
	}, &janus.JSEP{Type: "offer", SDP: offerSDP})
	if err != nil {
		return fmt.Errorf("%w: publish: %v", call.ErrCannotJoinRoom, err)
	}
	if configured.PluginData != nil {
		if errMsg, ok := configured.PluginData.Data["error"].(string); ok {
			errCode, _ := configured.PluginData.Data["error_code"].(float64)
			return fmt.Errorf("%w: publish: janus error %d: %s", call.ErrCannotJoinRoom, int(errCode), errMsg)
		}
	}
	if configured.JSEP == nil {
		return fmt.Errorf("%w: publish response missing JSEP answer", call.ErrCannotJoinRoom)
	}
	l.Infof("configured, got JSEP answer")

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  configured.JSEP.SDP,
	}); err != nil {
		return fmt.Errorf("%w: SetRemoteDescription: %v", call.ErrCannotJoinRoom, err)
	}

	go ivf.LoopIntoTrack(l, track, p.cameras.NewSource())

	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}

func (p *Plugin) runViewer(ctx context.Context, l *log.Logger, roomID int64, userID string) error {
	sessionID, err := p.client.CreateSession()
	if err != nil {
		return fmt.Errorf("%w: create session: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("session created: %d", sessionID)

	session := janus.NewSession(ctx, p.client, sessionID, l)
	defer session.Close()

	// Attach a publisher handle just to discover active publishers.
	pubHandleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		return fmt.Errorf("%w: attach pub handle: %v", call.ErrCannotJoinRoom, err)
	}

	joined, err := session.Send(pubHandleID, map[string]any{
		"request": "join",
		"ptype":   "publisher",
		"room":    roomID,
		"display": userID + "-discovery",
	}, nil)
	if err != nil {
		return fmt.Errorf("%w: publisher join: %v", call.ErrCannotJoinRoom, err)
	}
	if joined.PluginData == nil || joined.PluginData.Data["videoroom"] != "joined" {
		return fmt.Errorf("%w: unexpected publisher join response", call.ErrCannotJoinRoom)
	}

	pubs, _ := joined.PluginData.Data["publishers"].([]any)
	if len(pubs) == 0 {
		return fmt.Errorf("%w: no publishers in room %d", call.ErrCannotJoinRoom, roomID)
	}
	streams := make([]map[string]any, 0, len(pubs))
	for _, pub := range pubs {
		m, ok := pub.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := m["id"]; ok {
			streams = append(streams, map[string]any{"feed": id})
		}
	}
	if len(streams) == 0 {
		return fmt.Errorf("%w: could not extract publisher IDs", call.ErrCannotJoinRoom)
	}
	l.Infof("found %d publisher(s), subscribing", len(streams))

	subHandleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		return fmt.Errorf("%w: attach sub handle: %v", call.ErrCannotJoinRoom, err)
	}

	attached, err := session.Send(subHandleID, map[string]any{
		"request": "join",
		"ptype":   "subscriber",
		"room":    roomID,
		"streams": streams,
	}, nil)
	if err != nil {
		return fmt.Errorf("%w: subscriber join: %v", call.ErrCannotJoinRoom, err)
	}
	if attached.JSEP == nil {
		return fmt.Errorf("%w: subscriber join response missing JSEP offer", call.ErrCannotJoinRoom)
	}
	l.Infof("attached, got JSEP offer from Janus")

	pc, answerSDP, err := janus.StartPionSubscriber(l, p.clientIP, attached.JSEP.SDP)
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
			recDir := path.Join(p.recordingDirectory, fmt.Sprintf("/room=%d/user=%s", roomID, userID))
			var recErr error
			seg, recErr = vp9.NewIvfSegmenter(recDir)
			if recErr != nil {
				l.Errorf("[viewer] IvfSegmenter failed: %v", recErr)
			} else {
				seg.Enable()
				l.Infof("enabled IVF file writing for room=%d user=%s", roomID, userID)
			}
		}
		if _, err := p.viewerManager.SpawnViewer(track, receiver, userID, cfg, seg); err != nil {
			l.Errorf("[viewer] SpawnViewer failed: %v", err)
		}
	})

	done := make(chan struct{})
	var once sync.Once
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
		if s == webrtc.PeerConnectionStateFailed ||
			s == webrtc.PeerConnectionStateDisconnected ||
			s == webrtc.PeerConnectionStateClosed {
			once.Do(func() { close(done) })
		}
	})

	started, err := session.Send(subHandleID, map[string]any{
		"request": "start",
	}, &janus.JSEP{Type: "answer", SDP: answerSDP})
	if err != nil {
		return fmt.Errorf("%w: start: %v", call.ErrCannotJoinRoom, err)
	}
	if started.PluginData == nil {
		return fmt.Errorf("%w: unexpected start response", call.ErrCannotJoinRoom)
	}
	l.Infof("started, streaming")

	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}
