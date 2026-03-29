package janus

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/log"
	"call.zip/pkg/viewer"
	janus "call.zip/plugin/janus/internal"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
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
	svcConfig          call.SVCCameraConfig
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	pipeline           *call.StatsPipeline

	mu       sync.Mutex
	sessions []*janus.Session
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, e call.PluginEnv) error {
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

	pcapDir, err := call.SetupPacketCaptureDir(e.Config().Spec.Conference.PacketCapture)
	if err != nil {
		return fmt.Errorf("janus: %w", err)
	}
	p.packetCaptureDir = pcapDir

	// Validate that the conference name contains a parseable integer for the Janus room ID.
	if _, err := roomIDFromName(e.Config().Spec.Conference.Name); err != nil {
		return err
	}

	cams, err := call.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("janus: %w", err)
	}
	p.cameras = cams

	p.pipeline = call.NewStatsPipeline(e)

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
	p.mu.Lock()
	sessions := p.sessions
	p.sessions = nil
	p.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}

	p.pipeline.Stop()
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

	handleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		session.Close()
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
		session.Close()
		return fmt.Errorf("%w: join: %v", call.ErrCannotJoinRoom, err)
	}
	if joined.PluginData == nil || joined.PluginData.Data["videoroom"] != "joined" {
		session.Close()
		return fmt.Errorf("%w: unexpected join response: %v", call.ErrCannotJoinRoom, joined)
	}
	l.Infof("joined room %d as publisher", roomID)

	const initialBitrateBps = 3_500_000
	svc := ivf.ResolveSVC(p.cameras.Paths(), p.svcConfig.Mode, p.svcConfig.SpatialLayers, p.svcConfig.TemporalLayers, initialBitrateBps)

	var (
		pc        *webrtc.PeerConnection
		offerSDP  string
		startLoop func()
	)

	if svc.Enabled {
		var rtpTrack *webrtc.TrackLocalStaticRTP
		var getBitrate func() int
		pc, rtpTrack, getBitrate, offerSDP, err = janus.StartPionPublisher(l, p.clientIP, initialBitrateBps)
		if err != nil {
			session.Close()
			return fmt.Errorf("%w: pion publisher: %v", call.ErrCannotJoinRoom, err)
		}
		l.Infof("SVC config: %d spatial x %d temporal layers", svc.Config.NumSpatialLayers, svc.Config.NumTemporalLayers)
		startLoop = func() {
			go ivf.SVCLoopIntoTrack(l, rtpTrack, p.cameras.NewSource(), svc.Config, getBitrate)
		}
	} else {
		var sampleTrack *webrtc.TrackLocalStaticSample
		pc, sampleTrack, offerSDP, err = janus.StartSimplePublisher(l, p.clientIP)
		if err != nil {
			session.Close()
			return fmt.Errorf("%w: pion publisher: %v", call.ErrCannotJoinRoom, err)
		}
		startLoop = func() {
			go ivf.LoopIntoTrack(l, sampleTrack, p.cameras.NewSource())
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
		return fmt.Errorf("%w: publish: %v", call.ErrCannotJoinRoom, err)
	}
	if configured.PluginData != nil {
		if errMsg, ok := configured.PluginData.Data["error"].(string); ok {
			errCode, _ := configured.PluginData.Data["error_code"].(float64)
			session.Close()
			return fmt.Errorf("%w: publish: janus error %d: %s", call.ErrCannotJoinRoom, int(errCode), errMsg)
		}
	}
	if configured.JSEP == nil {
		session.Close()
		return fmt.Errorf("%w: publish response missing JSEP answer", call.ErrCannotJoinRoom)
	}
	l.Infof("configured, got JSEP answer")

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  configured.JSEP.SDP,
	}); err != nil {
		session.Close()
		return fmt.Errorf("%w: SetRemoteDescription: %v", call.ErrCannotJoinRoom, err)
	}

	startLoop()

	p.mu.Lock()
	p.sessions = append(p.sessions, session)
	p.mu.Unlock()

	return nil
}

func (p *Plugin) runViewer(ctx context.Context, l *log.Logger, roomID int64, userID string) error {
	sessionID, err := p.client.CreateSession()
	if err != nil {
		return fmt.Errorf("%w: create session: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("session created: %d", sessionID)

	session := janus.NewSession(ctx, p.client, sessionID, l)

	// Attach a publisher handle just to discover active publishers.
	pubHandleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		session.Close()
		return fmt.Errorf("%w: attach pub handle: %v", call.ErrCannotJoinRoom, err)
	}

	joined, err := session.Send(pubHandleID, map[string]any{
		"request": "join",
		"ptype":   "publisher",
		"room":    roomID,
		"display": userID + "-discovery",
	}, nil)
	if err != nil {
		session.Close()
		return fmt.Errorf("%w: publisher join: %v", call.ErrCannotJoinRoom, err)
	}
	if joined.PluginData == nil || joined.PluginData.Data["videoroom"] != "joined" {
		session.Close()
		return fmt.Errorf("%w: unexpected publisher join response", call.ErrCannotJoinRoom)
	}

	pubs, _ := joined.PluginData.Data["publishers"].([]any)
	if len(pubs) == 0 {
		session.Close()
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
		session.Close()
		return fmt.Errorf("%w: could not extract publisher IDs", call.ErrCannotJoinRoom)
	}
	l.Infof("found %d publisher(s), subscribing", len(streams))

	subHandleID, err := p.client.AttachPlugin(sessionID, videoroomPlugin)
	if err != nil {
		session.Close()
		return fmt.Errorf("%w: attach sub handle: %v", call.ErrCannotJoinRoom, err)
	}

	attached, err := session.Send(subHandleID, map[string]any{
		"request": "join",
		"ptype":   "subscriber",
		"room":    roomID,
		"streams": streams,
	}, nil)
	if err != nil {
		session.Close()
		return fmt.Errorf("%w: subscriber join: %v", call.ErrCannotJoinRoom, err)
	}
	if attached.JSEP == nil {
		session.Close()
		return fmt.Errorf("%w: subscriber join response missing JSEP offer", call.ErrCannotJoinRoom)
	}
	l.Infof("attached, got JSEP offer from Janus")

	pc, answerSDP, err := janus.StartPionSubscriber(l, p.clientIP, attached.JSEP.SDP)
	if err != nil {
		session.Close()
		return fmt.Errorf("%w: pion subscriber: %v", call.ErrCannotJoinRoom, err)
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
		return fmt.Errorf("%w: start: %v", call.ErrCannotJoinRoom, err)
	}
	if started.PluginData == nil {
		session.Close()
		return fmt.Errorf("%w: unexpected start response", call.ErrCannotJoinRoom)
	}
	l.Infof("started, streaming")

	p.mu.Lock()
	p.sessions = append(p.sessions, session)
	p.mu.Unlock()

	return nil
}
