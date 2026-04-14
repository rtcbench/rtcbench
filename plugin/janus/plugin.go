package janus

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench"
	"github.com/rtcbench/rtcbench/pkg/ivf"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/viewer"
	janus "github.com/rtcbench/rtcbench/plugin/janus/internal"
)

const (
	PluginID = "janus"

	cfgServerRoot    = "serverRoot"
	cfgAllowInsecure = "allowInsecureHttps"

	videoroomPlugin = "janus.plugin.videoroom"
)

type Plugin struct {
	client             *janus.Client
	log                *log.Logger
	logRegistry        *log.Registry
	serverIP           string
	clientIP           string
	cameras            *ivf.Cameras
	svcConfig          rtcbench.SVCCameraConfig
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	pipeline           *rtcbench.StatsPipeline
}

func NewPlugin() rtcbench.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, e rtcbench.PluginEnv) error {
	baseURL := e.Config().Spec.PluginConfig[PluginID].(map[string]any)[cfgServerRoot].(string)
	allowInsecure := e.Config().Spec.PluginConfig[PluginID].(map[string]any)[cfgAllowInsecure].(bool)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if allowInsecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient := &http.Client{Transport: transport}

	p.client = janus.NewClient(httpClient, baseURL)
	p.log = e.LogRegistry().NewLogger("janus", "")
	p.logRegistry = e.LogRegistry()

	info, err := p.client.GetInfo()
	if err != nil {
		return err
	}
	p.log.Infof("found Janus server: %q; version: %q", info["name"], info["version_string"])

	p.serverIP = e.Config().Spec.Network.ServerIP
	p.clientIP = e.Config().Spec.Network.ClientIP
	p.enableRecording = e.Config().Spec.Conference.Recording.Enabled
	p.recordingDirectory = e.Config().Spec.Conference.Recording.Directory
	p.statsBufferSize = e.Config().Spec.Conference.StatsBufferSize
	p.svcConfig = e.Config().Spec.Conference.Cameras.SVC

	pcapDir, err := rtcbench.SetupPacketCaptureDir(e.Config().Spec.Conference.PacketCapture)
	if err != nil {
		return fmt.Errorf("janus: %w", err)
	}
	p.packetCaptureDir = pcapDir

	// Validate that the conference name contains a parseable integer for the Janus room ID.
	if _, err := roomIDFromName(e.Config().Spec.Conference.Name); err != nil {
		return err
	}

	cams, err := rtcbench.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("janus: %w", err)
	}
	p.cameras = cams

	p.pipeline = rtcbench.NewStatsPipeline(e)

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
	p.pipeline.Stop()
	return nil
}

func (p *Plugin) NewParticipant(ctx context.Context, cfg *rtcbench.UserConfig) (rtcbench.Participant, error) {
	return &participant{
		plugin: p,
		userID: cfg.UserID,
		role:   cfg.Role,
		log:    p.logRegistry.NewLogger("janus", fmt.Sprintf("[%s][%s]", cfg.Role, cfg.UserID)),
	}, nil
}

type participant struct {
	mu               sync.Mutex
	plugin           *Plugin
	userID           string
	role             rtcbench.UserRole
	log              *log.Logger
	session          *janus.Session
	pc               *webrtc.PeerConnection
	startPublishLoop func() context.CancelFunc
	publishCancel    context.CancelFunc
}

func (p *participant) JoinRoom(ctx context.Context, req *rtcbench.JoinRequest) error {
	if req == nil || req.RoomID == "" {
		return rtcbench.ErrMissingRoomID
	}

	roomID, err := roomIDFromName(req.RoomID)
	if err != nil {
		return err
	}

	p.mu.Lock()
	if p.session != nil {
		p.mu.Unlock()
		return rtcbench.ErrConnectionExists
	}
	p.mu.Unlock()

	var (
		session          *janus.Session
		pc               *webrtc.PeerConnection
		startPublishLoop func() context.CancelFunc
	)
	switch p.role {
	case rtcbench.Sender:
		session, pc, startPublishLoop, err = p.plugin.joinSender(ctx, p.log, roomID, p.userID)
	case rtcbench.Viewer:
		session, pc, err = p.plugin.joinViewer(ctx, p.log, roomID, p.userID)
	default:
		return rtcbench.ErrUnsupportedRole
	}
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.session = session
	p.pc = pc
	p.startPublishLoop = startPublishLoop
	p.mu.Unlock()
	return nil
}

func (p *participant) LeaveRoom(ctx context.Context, req *rtcbench.LeaveRequest) error {
	return p.Close()
}

func (p *participant) Close() error {
	p.mu.Lock()
	session := p.session
	pc := p.pc
	publishCancel := p.publishCancel
	p.session = nil
	p.pc = nil
	p.startPublishLoop = nil
	p.publishCancel = nil
	p.mu.Unlock()

	if publishCancel != nil {
		publishCancel()
	}
	if session != nil {
		session.Close()
	}
	if pc != nil {
		return pc.Close()
	}
	return nil
}

func (p *participant) PublishVideo(ctx context.Context, req *rtcbench.PublishVideoRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.role != rtcbench.Sender {
		return rtcbench.ErrUnsupportedCapability
	}
	if p.session == nil || p.pc == nil {
		return fmt.Errorf("%w: participant not joined", rtcbench.ErrCannotJoinRoom)
	}
	if p.publishCancel != nil {
		return nil
	}
	if p.startPublishLoop == nil {
		return rtcbench.ErrUnsupportedCapability
	}
	p.publishCancel = p.startPublishLoop()
	return nil
}

func (p *participant) UnpublishVideo(ctx context.Context, req *rtcbench.UnpublishVideoRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.role != rtcbench.Sender {
		return rtcbench.ErrUnsupportedCapability
	}
	if p.publishCancel == nil {
		return nil
	}
	p.publishCancel()
	p.publishCancel = nil
	return nil
}

func (p *Plugin) joinSender(ctx context.Context, l *log.Logger, roomID int64, userID string) (*janus.Session, *webrtc.PeerConnection, func() context.CancelFunc, error) {
	if p.cameras == nil {
		return nil, nil, nil, fmt.Errorf("%w: no IVF files configured for sender", rtcbench.ErrCannotJoinRoom)
	}

	sessionID, err := p.client.CreateSession()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: create session: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("session created: %d", sessionID)

	session := janus.NewSession(ctx, p.client, sessionID, l)

	handleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		session.Close()
		return nil, nil, nil, fmt.Errorf("%w: attach plugin: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("handle attached: %d", handleID)

	joined, err := session.Send(handleID, map[string]any{
		"request": "join",
		"ptype":   "publisher",
		"room":    roomID,
		"display": userID,
	}, nil)
	if err != nil {
		session.Close()
		return nil, nil, nil, fmt.Errorf("%w: join: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	if joined.PluginData == nil || joined.PluginData.Data["videoroom"] != "joined" {
		session.Close()
		return nil, nil, nil, fmt.Errorf("%w: unexpected join response: %v", rtcbench.ErrCannotJoinRoom, joined)
	}
	l.Infof("joined room %d as publisher", roomID)

	const initialBitrateBps = 3_500_000
	svc := ivf.ResolveSVC(p.cameras.Paths(), p.svcConfig.Mode, p.svcConfig.SpatialLayers, p.svcConfig.TemporalLayers, initialBitrateBps)

	var (
		pc        *webrtc.PeerConnection
		offerSDP  string
		startLoop func() context.CancelFunc
	)

	if svc.Enabled {
		var rtpTrack *webrtc.TrackLocalStaticRTP
		var getBitrate func() int
		pc, rtpTrack, getBitrate, offerSDP, err = janus.StartPionPublisher(l, p.clientIP, initialBitrateBps)
		if err != nil {
			session.Close()
			return nil, nil, nil, fmt.Errorf("%w: pion publisher: %v", rtcbench.ErrCannotJoinRoom, err)
		}
		l.Infof("SVC config: %d spatial x %d temporal layers", svc.Config.NumSpatialLayers, svc.Config.NumTemporalLayers)
		startLoop = func() context.CancelFunc {
			loopCtx, cancel := context.WithCancel(context.Background())
			go ivf.SVCLoopIntoTrackUntil(loopCtx, l, rtpTrack, p.cameras.NewSource(), svc.Config, getBitrate)
			return cancel
		}
	} else {
		var sampleTrack *webrtc.TrackLocalStaticSample
		pc, sampleTrack, offerSDP, err = janus.StartSimplePublisher(l, p.clientIP)
		if err != nil {
			session.Close()
			return nil, nil, nil, fmt.Errorf("%w: pion publisher: %v", rtcbench.ErrCannotJoinRoom, err)
		}
		startLoop = func() context.CancelFunc {
			loopCtx, cancel := context.WithCancel(context.Background())
			go ivf.LoopIntoTrackUntil(loopCtx, l, sampleTrack, p.cameras.NewSource())
			return cancel
		}
	}

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
	})

	configured, err := session.Send(handleID, map[string]any{
		"request":    "publish",
		"videocodec": "vp9",
	}, &janus.JSEP{Type: "offer", SDP: offerSDP})
	if err != nil {
		session.Close()
		_ = pc.Close()
		return nil, nil, nil, fmt.Errorf("%w: publish: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	if configured.PluginData != nil {
		if errMsg, ok := configured.PluginData.Data["error"].(string); ok {
			errCode, _ := configured.PluginData.Data["error_code"].(float64)
			session.Close()
			_ = pc.Close()
			return nil, nil, nil, fmt.Errorf("%w: publish: janus error %d: %s", rtcbench.ErrCannotJoinRoom, int(errCode), errMsg)
		}
	}
	if configured.JSEP == nil {
		session.Close()
		_ = pc.Close()
		return nil, nil, nil, fmt.Errorf("%w: publish response missing JSEP answer", rtcbench.ErrCannotJoinRoom)
	}
	l.Infof("configured, got JSEP answer")

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  configured.JSEP.SDP,
	}); err != nil {
		session.Close()
		_ = pc.Close()
		return nil, nil, nil, fmt.Errorf("%w: SetRemoteDescription: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	return session, pc, startLoop, nil
}

func (p *Plugin) joinViewer(ctx context.Context, l *log.Logger, roomID int64, userID string) (*janus.Session, *webrtc.PeerConnection, error) {
	sessionID, err := p.client.CreateSession()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: create session: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("session created: %d", sessionID)

	session := janus.NewSession(ctx, p.client, sessionID, l)

	// Attach a publisher handle just to discover active publishers.
	pubHandleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("%w: attach pub handle: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	joined, err := session.Send(pubHandleID, map[string]any{
		"request": "join",
		"ptype":   "publisher",
		"room":    roomID,
		"display": userID + "-discovery",
	}, nil)
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("%w: publisher join: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	if joined.PluginData == nil || joined.PluginData.Data["videoroom"] != "joined" {
		session.Close()
		return nil, nil, fmt.Errorf("%w: unexpected publisher join response", rtcbench.ErrCannotJoinRoom)
	}

	pubs, _ := joined.PluginData.Data["publishers"].([]any)
	if len(pubs) == 0 {
		session.Close()
		return nil, nil, fmt.Errorf("%w: no publishers in room %d", rtcbench.ErrCannotJoinRoom, roomID)
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
		session.Close()
		return nil, nil, fmt.Errorf("%w: could not extract publisher IDs", rtcbench.ErrCannotJoinRoom)
	}
	l.Infof("found %d publisher(s), subscribing", len(streams))

	subHandleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("%w: attach sub handle: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	attached, err := session.Send(subHandleID, map[string]any{
		"request": "join",
		"ptype":   "subscriber",
		"room":    roomID,
		"streams": streams,
	}, nil)
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("%w: subscriber join: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	if attached.JSEP == nil {
		session.Close()
		return nil, nil, fmt.Errorf("%w: subscriber join response missing JSEP offer", rtcbench.ErrCannotJoinRoom)
	}
	l.Infof("attached, got JSEP offer from Janus")

	pc, answerSDP, err := janus.StartPionSubscriber(l, p.clientIP, attached.JSEP.SDP)
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("%w: pion subscriber: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		l.Infof("[pion] OnTrack: %s %s PT=%d", track.Kind(), track.Codec().MimeType, track.PayloadType())
		ssrc := track.SSRC()
		p.pipeline.ViewerManager.HandleTrack(track, receiver, p.pipeline.Publisher, viewer.TrackHandlerOpts{
			StatsBufferSize:    p.statsBufferSize,
			PacketCaptureDir:   p.packetCaptureDir,
			EnableRecording:    p.enableRecording,
			RecordingDirectory: p.recordingDirectory,
			RoomID:             fmt.Sprintf("%d", roomID),
			UserID:             userID,
			Logger:             l,
			SendPLI: func() {
				if writeErr := pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(ssrc)}}); writeErr != nil {
					l.Errorf("[rtcp] WritePLI: %v", writeErr)
				} else {
					l.Infof("[rtcp] sent PLI ssrc=%d", ssrc)
				}
			},
		})
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
	})

	started, err := session.Send(subHandleID, map[string]any{
		"request": "start",
	}, &janus.JSEP{Type: "answer", SDP: answerSDP})
	if err != nil {
		session.Close()
		_ = pc.Close()
		return nil, nil, fmt.Errorf("%w: start: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	if started.PluginData == nil {
		session.Close()
		_ = pc.Close()
		return nil, nil, fmt.Errorf("%w: unexpected start response", rtcbench.ErrCannotJoinRoom)
	}
	l.Infof("started, streaming")

	return session, pc, nil
}
