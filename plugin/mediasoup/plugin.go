package mediasoup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench"
	"github.com/rtcbench/rtcbench/pkg/ivf"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/viewer"
	ms "github.com/rtcbench/rtcbench/plugin/mediasoup/internal"
)

const (
	PluginID = "mediasoup"

	cfgServerURL = "serverURL"
)

// Plugin implements rtcbench.Plugin for the mediasoup SFU.
type Plugin struct {
	serverURL          string
	clientIP           string
	cameras            *ivf.Cameras
	svcConfig          rtcbench.SVCCameraConfig
	log                *log.Logger
	logRegistry        *log.Registry
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	pipeline           *rtcbench.StatsPipeline

	routerCaps ms.RtpCapabilities
	vp9PT      uint8 // VP9 payload type from router capabilities
}

// NewPlugin returns a zero-initialized mediasoup plugin.
func NewPlugin() rtcbench.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, e rtcbench.PluginEnv) error {
	p.serverURL = e.Config().Spec.PluginConfig[PluginID].(map[string]any)[cfgServerURL].(string)
	p.log = e.LogRegistry().NewLogger("mediasoup", "")
	p.logRegistry = e.LogRegistry()
	p.clientIP = e.Config().Spec.Network.ClientIP
	p.enableRecording = e.Config().Spec.Conference.Recording.Enabled
	p.recordingDirectory = e.Config().Spec.Conference.Recording.Directory
	p.statsBufferSize = e.Config().Spec.Conference.StatsBufferSize
	p.svcConfig = e.Config().Spec.Conference.Cameras.SVC

	pcapDir, err := rtcbench.SetupPacketCaptureDir(e.Config().Spec.Conference.PacketCapture)
	if err != nil {
		return fmt.Errorf("mediasoup: %w", err)
	}
	p.packetCaptureDir = pcapDir

	cams, err := rtcbench.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("mediasoup: %w", err)
	}
	p.cameras = cams

	// Probe the mediasoup server to discover router capabilities.
	caps, err := p.probeRouterCapabilities(ctx)
	if err != nil {
		return fmt.Errorf("mediasoup: probe server: %w", err)
	}
	p.routerCaps = caps

	// Find VP9 payload type.
	for _, c := range caps.Codecs {
		if strings.EqualFold(c.MimeType, "video/VP9") {
			p.vp9PT = uint8(c.PreferredPayloadType)
			break
		}
	}
	if p.vp9PT == 0 {
		return fmt.Errorf("mediasoup: server does not support VP9")
	}
	p.log.Infof("router supports VP9 (PT=%d), %d header extensions", p.vp9PT, len(caps.HeaderExtensions))

	p.pipeline = rtcbench.NewStatsPipeline(e)

	return nil
}

// unmarshalRouterCaps extracts RtpCapabilities from the mediasoup-demo
// getRouterRtpCapabilities response, which wraps them in
// {"routerRtpCapabilities": {...}}.
func unmarshalRouterCaps(data json.RawMessage) (ms.RtpCapabilities, error) {
	var wrapper struct {
		RouterRtpCapabilities ms.RtpCapabilities `json:"routerRtpCapabilities"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return ms.RtpCapabilities{}, fmt.Errorf("unmarshal capabilities: %w", err)
	}
	return wrapper.RouterRtpCapabilities, nil
}

// probeRouterCapabilities connects briefly to discover what codecs the
// mediasoup router supports.
func (p *Plugin) probeRouterCapabilities(ctx context.Context) (ms.RtpCapabilities, error) {
	url := fmt.Sprintf("%s/?roomId=__probe__&peerId=__probe__", p.serverURL)
	protoo, err := ms.NewProtoo(ctx, url, p.log, nil, nil)
	if err != nil {
		return ms.RtpCapabilities{}, err
	}
	defer protoo.Close()

	data, err := protoo.Request("getRouterRtpCapabilities", nil)
	if err != nil {
		return ms.RtpCapabilities{}, err
	}
	return unmarshalRouterCaps(data)
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
		log:    p.logRegistry.NewLogger("mediasoup", fmt.Sprintf("[%s][%s]", cfg.Role, cfg.UserID)),
	}, nil
}

type participant struct {
	mu               sync.Mutex
	plugin           *Plugin
	userID           string
	role             rtcbench.UserRole
	log              *log.Logger
	protoo           *ms.Protoo
	pc               *webrtc.PeerConnection
	startPublishLoop func() context.CancelFunc
	publishCancel    context.CancelFunc
}

func (p *participant) JoinRoom(ctx context.Context, req *rtcbench.JoinRequest) error {
	if req == nil || req.RoomID == "" {
		return rtcbench.ErrMissingRoomID
	}

	p.mu.Lock()
	if p.protoo != nil {
		p.mu.Unlock()
		return rtcbench.ErrConnectionExists
	}
	p.mu.Unlock()

	var (
		protoo           *ms.Protoo
		pc               *webrtc.PeerConnection
		startPublishLoop func() context.CancelFunc
		err              error
	)
	switch p.role {
	case rtcbench.Sender:
		protoo, pc, startPublishLoop, err = p.plugin.joinSender(ctx, p.log, req.RoomID, p.userID)
	case rtcbench.Viewer:
		protoo, pc, err = p.plugin.joinViewer(ctx, p.log, req.RoomID, p.userID)
	default:
		return rtcbench.ErrUnsupportedRole
	}
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.protoo = protoo
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
	protoo := p.protoo
	pc := p.pc
	publishCancel := p.publishCancel
	p.protoo = nil
	p.pc = nil
	p.startPublishLoop = nil
	p.publishCancel = nil
	p.mu.Unlock()

	if publishCancel != nil {
		publishCancel()
	}
	if protoo != nil {
		protoo.Close()
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
	if p.protoo == nil || p.pc == nil {
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

// ---------------------------------------------------------------------------
// sender
// ---------------------------------------------------------------------------

func (p *Plugin) joinSender(ctx context.Context, l *log.Logger, roomID, userID string) (*ms.Protoo, *webrtc.PeerConnection, func() context.CancelFunc, error) {
	if p.cameras == nil {
		return nil, nil, nil, fmt.Errorf("%w: no IVF files configured for sender", rtcbench.ErrCannotJoinRoom)
	}

	// 1. Connect protoo.
	url := fmt.Sprintf("%s/?roomId=%s&peerId=%s", p.serverURL, roomID, userID)
	protoo, err := ms.NewProtoo(ctx, url, l, nil, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: protoo: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("protoo connected")

	// 2. Get router RTP capabilities.
	capsData, err := protoo.Request("getRouterRtpCapabilities", nil)
	if err != nil {
		protoo.Close()
		return nil, nil, nil, fmt.Errorf("%w: getRouterRtpCapabilities: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	caps, err := unmarshalRouterCaps(capsData)
	if err != nil {
		protoo.Close()
		return nil, nil, nil, fmt.Errorf("%w: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	// 3. Create send transport.
	transportData, err := protoo.Request("createWebRtcTransport", map[string]any{
		"forceTcp": false,
		"appData":  map[string]any{"direction": "producer"},
	})
	if err != nil {
		protoo.Close()
		return nil, nil, nil, fmt.Errorf("%w: createWebRtcTransport: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	var transport ms.TransportOptions
	if err := json.Unmarshal(transportData, &transport); err != nil {
		protoo.Close()
		return nil, nil, nil, fmt.Errorf("%w: unmarshal transport: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("send transport created: %s", transport.ID)

	// 4. Join room.
	_, err = protoo.Request("join", map[string]any{
		"displayName":     userID,
		"rtpCapabilities": caps,
		"device":          map[string]any{"name": "RTCBench", "flag": "go"},
	})
	if err != nil {
		protoo.Close()
		return nil, nil, nil, fmt.Errorf("%w: join: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("joined room %s", roomID)

	// 5. Create pion PeerConnection with VP9 send track.
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
		pc, rtpTrack, getBitrate, offerSDP, err = ms.StartSendPC(l, p.clientIP, p.vp9PT, initialBitrateBps)
		if err != nil {
			protoo.Close()
			return nil, nil, nil, fmt.Errorf("%w: pion send: %v", rtcbench.ErrCannotJoinRoom, err)
		}
		l.Infof("SVC config: %d spatial x %d temporal layers", svc.Config.NumSpatialLayers, svc.Config.NumTemporalLayers)
		startLoop = func() context.CancelFunc {
			loopCtx, cancel := context.WithCancel(context.Background())
			go ivf.SVCLoopIntoTrackUntil(loopCtx, l, rtpTrack, p.cameras.NewSource(), svc.Config, getBitrate)
			return cancel
		}
	} else {
		var sampleTrack *webrtc.TrackLocalStaticSample
		pc, sampleTrack, offerSDP, err = ms.StartSendPCSimple(l, p.clientIP, p.vp9PT)
		if err != nil {
			protoo.Close()
			return nil, nil, nil, fmt.Errorf("%w: pion send: %v", rtcbench.ErrCannotJoinRoom, err)
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

	// 6. Build fake SDP answer from mediasoup transport params.
	answerSDP := ms.BuildSendAnswerSDP(offerSDP, transport)
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		protoo.Close()
		_ = pc.Close()
		return nil, nil, nil, fmt.Errorf("%w: SetRemoteDescription: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("remote description set (fake answer)")

	// 7. Connect transport with our DTLS fingerprint.
	localFP := ms.ExtractDtlsFingerprint(pc.LocalDescription().SDP)
	_, err = protoo.Request("connectWebRtcTransport", map[string]any{
		"transportId": transport.ID,
		"dtlsParameters": ms.DtlsParameters{
			Role:         "client",
			Fingerprints: []ms.DtlsFingerprint{localFP},
		},
	})
	if err != nil {
		protoo.Close()
		_ = pc.Close()
		return nil, nil, nil, fmt.Errorf("%w: connectWebRtcTransport: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("transport connected")

	// 8. Produce.
	rtpParams := ms.ExtractRtpParameters(offerSDP, caps)
	produceData, err := protoo.Request("produce", map[string]any{
		"transportId":   transport.ID,
		"kind":          "video",
		"rtpParameters": rtpParams,
		"appData":       map[string]any{"source": "webcam"},
	})
	if err != nil {
		protoo.Close()
		_ = pc.Close()
		return nil, nil, nil, fmt.Errorf("%w: produce: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	var result ms.ProduceResult
	json.Unmarshal(produceData, &result)
	l.Infof("producing [id:%s]", result.ID)

	return protoo, pc, startLoop, nil
}

// ---------------------------------------------------------------------------
// viewer
// ---------------------------------------------------------------------------

func (p *Plugin) joinViewer(ctx context.Context, l *log.Logger, roomID, userID string) (*ms.Protoo, *webrtc.PeerConnection, error) {
	// State for recv transport SDP renegotiation.
	var (
		protooClient *ms.Protoo
		recvPC       *webrtc.PeerConnection
		consumers    []ms.ConsumerData
		transport    ms.TransportOptions
		connected    bool
		sdpVersion   int
		mu           sync.Mutex
	)

	// Handler for server-initiated "newConsumer" requests.
	onRequest := func(method string, data json.RawMessage) (any, error) {
		if method != "newConsumer" {
			l.Infof("[protoo] ignoring server request: %s", method)
			return struct{}{}, nil
		}

		var consumer ms.ConsumerData
		if err := json.Unmarshal(data, &consumer); err != nil {
			return nil, fmt.Errorf("unmarshal newConsumer: %w", err)
		}
		l.Infof("newConsumer [id:%s, kind:%s, peer:%s]", consumer.ID, consumer.Kind, consumer.PeerID)

		mu.Lock()
		defer mu.Unlock()

		consumers = append(consumers, consumer)
		sdpVersion++

		// Build offer SDP with all consumers and negotiate with pion.
		offerSDP := ms.BuildRecvOfferSDP(transport, consumers, sdpVersion)
		answerSDP, err := ms.NegotiateRecvSDP(recvPC, offerSDP)
		if err != nil {
			return nil, fmt.Errorf("negotiate recv: %w", err)
		}

		// Connect transport on first consumer.
		if !connected {
			localFP := ms.ExtractDtlsFingerprint(answerSDP)
			_, err = protooClient.Request("connectWebRtcTransport", map[string]any{
				"transportId": transport.ID,
				"dtlsParameters": ms.DtlsParameters{
					Role:         "client",
					Fingerprints: []ms.DtlsFingerprint{localFP},
				},
			})
			if err != nil {
				return nil, fmt.Errorf("connectWebRtcTransport: %w", err)
			}
			l.Infof("recv transport connected")
			connected = true
		}

		// Resume the consumer to start receiving media.
		if err := protooClient.Notify("resumeConsumer", map[string]any{
			"consumerId": consumer.ID,
		}); err != nil {
			l.Errorf("resumeConsumer notify failed: %v", err)
		}

		return struct{}{}, nil
	}

	onNotify := func(method string, data json.RawMessage) {
		l.Infof("[notification] %s", method)
	}

	// 1. Connect protoo.
	url := fmt.Sprintf("%s/?roomId=%s&peerId=%s", p.serverURL, roomID, userID)
	var err error
	protooClient, err = ms.NewProtoo(ctx, url, l, onRequest, onNotify)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: protoo: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("protoo connected")

	// 2. Get router capabilities.
	capsData, err := protooClient.Request("getRouterRtpCapabilities", nil)
	if err != nil {
		protooClient.Close()
		return nil, nil, fmt.Errorf("%w: getRouterRtpCapabilities: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	caps, capsErr := unmarshalRouterCaps(capsData)
	if capsErr != nil {
		protooClient.Close()
		return nil, nil, fmt.Errorf("%w: %v", rtcbench.ErrCannotJoinRoom, capsErr)
	}

	// 3. Create recv transport.
	transportData, err := protooClient.Request("createWebRtcTransport", map[string]any{
		"forceTcp": false,
		"appData":  map[string]any{"direction": "consumer"},
	})
	if err != nil {
		protooClient.Close()
		return nil, nil, fmt.Errorf("%w: createWebRtcTransport: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	json.Unmarshal(transportData, &transport)
	l.Infof("recv transport created: %s", transport.ID)

	// 4. Create recv PeerConnection.
	recvPC, err = ms.StartRecvPC(l, p.clientIP, p.vp9PT)
	if err != nil {
		protooClient.Close()
		return nil, nil, fmt.Errorf("%w: pion recv: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	// 5. Set up track handler (fires after SDP negotiation adds consumer tracks).
	recvPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		l.Infof("[pion] OnTrack: %s %s PT=%d SSRC=%d", track.Kind(), track.Codec().MimeType, track.PayloadType(), track.SSRC())
		ssrc := track.SSRC()
		p.pipeline.ViewerManager.HandleTrack(track, receiver, p.pipeline.Publisher, viewer.TrackHandlerOpts{
			StatsBufferSize:    p.statsBufferSize,
			PacketCaptureDir:   p.packetCaptureDir,
			EnableRecording:    p.enableRecording,
			RecordingDirectory: p.recordingDirectory,
			RoomID:             roomID,
			UserID:             userID,
			Logger:             l,
			SendPLI: func() {
				if writeErr := recvPC.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(ssrc)}}); writeErr != nil {
					l.Errorf("[rtcp] WritePLI: %v", writeErr)
				} else {
					l.Infof("[rtcp] sent PLI ssrc=%d", ssrc)
				}
			},
		})
	})

	recvPC.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
	})

	// 6. Join room — triggers newConsumer requests for existing producers.
	_, err = protooClient.Request("join", map[string]any{
		"displayName":     userID,
		"rtpCapabilities": caps,
		"device":          map[string]any{"name": "RTCBench", "flag": "go"},
	})
	if err != nil {
		protooClient.Close()
		_ = recvPC.Close()
		return nil, nil, fmt.Errorf("%w: join: %v", rtcbench.ErrCannotJoinRoom, err)
	}
	l.Infof("joined room %s", roomID)

	return protooClient, recvPC, nil
}
