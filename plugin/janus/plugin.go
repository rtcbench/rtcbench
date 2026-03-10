package janus

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/viewer"
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
	statsInputChanSize      = 128
)

type Plugin struct {
	client        *janus.Client
	serverIP      string
	clientIP      string
	ivfPaths      []string
	viewerManager *viewer.Manager
	publisher     *vp9_stats.Publisher
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

	info, err := p.client.GetInfo()
	if err != nil {
		return err
	}
	log.Printf("Found Janus server: %q; Version: %q", info["name"], info["version_string"])

	p.serverIP = config.Spec.Network.ServerIP
	p.clientIP = config.Spec.Network.ClientIP

	// Validate that the conference name contains a parseable integer for the Janus room ID.
	if _, err := roomIDFromName(config.Spec.Conference.Name); err != nil {
		return err
	}

	// Pre-load IVF paths for senders.
	if config.Spec.Conference.Cameras.PerRoom > 0 {
		paths, err := ivf.LoadCameraPaths(config.Spec.Conference.Cameras.Directory)
		if err != nil {
			return fmt.Errorf("janus: %w", err)
		}
		p.ivfPaths = paths
	}

	// Stats pipeline shared across all viewer goroutines.
	statsInput := make(chan vp9_stats.VideoQualitySample, statsInputChanSize)
	p.viewerManager = viewer.NewManager(statsInput)
	p.publisher = vp9_stats.NewPublisher(statsInput)
	p.publisher.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		log.Printf("[sub-data-v1] period=%s,sample=%s", period.String(), sample.String())
	})
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

	logf := func(format string, args ...any) {
		log.Printf("[janus][%s][%s] "+format, append([]any{string(role), userID}, args...)...)
	}

	switch role {
	case call.Sender:
		return p.runSender(ctx, logf, roomInt, userID)
	case call.Viewer:
		return p.runViewer(ctx, logf, roomInt, userID)
	default:
		return call.ErrUnsupportedRole
	}
}

func (p *Plugin) runSender(ctx context.Context, logf func(string, ...any), roomID int64, userID string) error {
	if len(p.ivfPaths) == 0 {
		return fmt.Errorf("%w: no IVF files configured for sender", call.ErrCannotJoinRoom)
	}

	sessionID, err := p.client.CreateSession()
	if err != nil {
		return fmt.Errorf("%w: create session: %v", call.ErrCannotJoinRoom, err)
	}
	logf("session created: %d", sessionID)

	session := janus.NewSession(ctx, p.client, sessionID)
	defer session.Close()

	handleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		return fmt.Errorf("%w: attach plugin: %v", call.ErrCannotJoinRoom, err)
	}
	logf("handle attached: %d", handleID)

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
	logf("joined room %d as publisher", roomID)

	pc, track, offerSDP, err := janus.StartPionPublisher(logf, p.clientIP, p.ivfPaths)
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
	logf("configured, got JSEP answer")

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  configured.JSEP.SDP,
	}); err != nil {
		return fmt.Errorf("%w: SetRemoteDescription: %v", call.ErrCannotJoinRoom, err)
	}

	if err := janus.AddStaticCandidate(pc, p.serverIP); err != nil {
		return fmt.Errorf("%w: AddICECandidate: %v", call.ErrCannotJoinRoom, err)
	}

	go ivf.LoopIntoTrack(logf, track, p.ivfPaths)

	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}

func (p *Plugin) runViewer(ctx context.Context, logf func(string, ...any), roomID int64, userID string) error {
	sessionID, err := p.client.CreateSession()
	if err != nil {
		return fmt.Errorf("%w: create session: %v", call.ErrCannotJoinRoom, err)
	}
	logf("session created: %d", sessionID)

	session := janus.NewSession(ctx, p.client, sessionID)
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
	logf("found %d publisher(s), subscribing", len(streams))

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
	logf("attached, got JSEP offer from Janus")

	pc, answerSDP, err := janus.StartPionSubscriber(logf, p.clientIP, attached.JSEP.SDP)
	if err != nil {
		return fmt.Errorf("%w: pion subscriber: %v", call.ErrCannotJoinRoom, err)
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logf("[pion] OnTrack: %s %s PT=%d", track.Kind(), track.Codec().MimeType, track.PayloadType())
		cfg := &viewer.Config{
			PacketsPerSample:  viewerPacketsPerSample,
			VP9RTPPayloadType: int(track.PayloadType()),
			TrackBufferSize:   viewerTrackBufferSize,
		}
		if _, err := p.viewerManager.SpawnViewer(track, receiver, userID, cfg, nil); err != nil {
			logf("[viewer] SpawnViewer failed: %v", err)
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

	if err := janus.AddStaticCandidate(pc, p.serverIP); err != nil {
		return fmt.Errorf("%w: AddICECandidate: %v", call.ErrCannotJoinRoom, err)
	}

	started, err := session.Send(subHandleID, map[string]any{
		"request": "start",
	}, &janus.JSEP{Type: "answer", SDP: answerSDP})
	if err != nil {
		return fmt.Errorf("%w: start: %v", call.ErrCannotJoinRoom, err)
	}
	if started.PluginData == nil {
		return fmt.Errorf("%w: unexpected start response", call.ErrCannotJoinRoom)
	}
	logf("started, streaming")

	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}
