package mediasoup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"call.zip"
	"call.zip/pkg/ivf"
	"call.zip/pkg/log"
	"call.zip/pkg/viewer"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
	ms "call.zip/plugin/mediasoup/internal"
	"github.com/pion/webrtc/v3"
)

const (
	PluginID = "mediasoup"

	cfgServerURL = "serverURL"

	viewerPacketsPerSample = 1000
	viewerTrackBufferSize  = 1500
	statsInputChanSize     = 1 << 16 // 65536
)

// Plugin implements call.Plugin for the mediasoup SFU.
type Plugin struct {
	serverURL          string
	clientIP           string
	cameras            *ivf.Cameras
	log                *log.Logger
	logRegistry        *log.Registry
	enableRecording    bool
	recordingDirectory string
	packetCaptureDir   string
	statsBufferSize    int
	viewerManager      *viewer.Manager
	publisher          *vp9_stats.Publisher

	routerCaps ms.RtpCapabilities
	vp9PT      uint8 // VP9 payload type from router capabilities

	mu      sync.Mutex
	protoos []*ms.Protoo
}

// NewPlugin returns a zero-initialized mediasoup plugin.
func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, config *call.Config) error {
	p.serverURL = config.Spec.PluginConfig[PluginID].(map[string]any)[cfgServerURL].(string)
	p.log = config.Log.NewLogger("mediasoup", "")
	p.logRegistry = config.Log
	p.clientIP = config.Spec.Network.ClientIP
	p.enableRecording = config.Spec.Conference.Recording.Enabled
	p.recordingDirectory = config.Spec.Conference.Recording.Directory
	p.statsBufferSize = config.Spec.Conference.StatsBufferSize

	if config.Spec.Conference.PacketCapture.Enabled {
		ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
		p.packetCaptureDir = filepath.Join(config.Spec.Conference.PacketCapture.Directory, ts)
		if err := os.MkdirAll(p.packetCaptureDir, 0o755); err != nil {
			return fmt.Errorf("mediasoup: packet capture directory: %w", err)
		}
	}

	// Load IVF camera files for senders.
	if config.Spec.Conference.Cameras.PerRoom > 0 {
		cams, err := ivf.NewCameras(config.Spec.Conference.Cameras.Directory, config.Spec.Conference.Cameras.InMemory)
		if err != nil {
			return fmt.Errorf("mediasoup: %w", err)
		}
		p.cameras = cams
	}

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
	p.mu.Lock()
	protoos := p.protoos
	p.protoos = nil
	p.mu.Unlock()

	for _, pr := range protoos {
		pr.Close()
	}

	if p.viewerManager != nil {
		p.viewerManager.StopAll()
	}
	if p.publisher != nil {
		p.publisher.Stop()
	}
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	l := p.logRegistry.NewLogger("mediasoup", fmt.Sprintf("[%s][%s]", role, userID))

	switch role {
	case call.Sender:
		return p.runSender(ctx, l, roomID, userID)
	case call.Viewer:
		return p.runViewer(ctx, l, roomID, userID)
	default:
		return call.ErrUnsupportedRole
	}
}

// ---------------------------------------------------------------------------
// sender
// ---------------------------------------------------------------------------

func (p *Plugin) runSender(ctx context.Context, l *log.Logger, roomID, userID string) error {
	if p.cameras == nil {
		return fmt.Errorf("%w: no IVF files configured for sender", call.ErrCannotJoinRoom)
	}

	// 1. Connect protoo.
	url := fmt.Sprintf("%s/?roomId=%s&peerId=%s", p.serverURL, roomID, userID)
	protoo, err := ms.NewProtoo(ctx, url, l, nil, nil)
	if err != nil {
		return fmt.Errorf("%w: protoo: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("protoo connected")

	// 2. Get router RTP capabilities.
	capsData, err := protoo.Request("getRouterRtpCapabilities", nil)
	if err != nil {
		protoo.Close()
		return fmt.Errorf("%w: getRouterRtpCapabilities: %v", call.ErrCannotJoinRoom, err)
	}
	caps, err := unmarshalRouterCaps(capsData)
	if err != nil {
		protoo.Close()
		return fmt.Errorf("%w: %v", call.ErrCannotJoinRoom, err)
	}

	// 3. Create send transport.
	transportData, err := protoo.Request("createWebRtcTransport", map[string]any{
		"forceTcp": false,
		"appData":  map[string]any{"direction": "producer"},
	})
	if err != nil {
		protoo.Close()
		return fmt.Errorf("%w: createWebRtcTransport: %v", call.ErrCannotJoinRoom, err)
	}
	var transport ms.TransportOptions
	if err := json.Unmarshal(transportData, &transport); err != nil {
		protoo.Close()
		return fmt.Errorf("%w: unmarshal transport: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("send transport created: %s", transport.ID)

	// 4. Join room.
	_, err = protoo.Request("join", map[string]any{
		"displayName":     userID,
		"rtpCapabilities": caps,
		"device":          map[string]any{"name": "CallZIP", "flag": "go"},
	})
	if err != nil {
		protoo.Close()
		return fmt.Errorf("%w: join: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("joined room %s", roomID)

	// 5. Create pion PeerConnection with VP9 send track.
	pc, track, offerSDP, err := ms.StartSendPC(l, p.clientIP, p.vp9PT)
	if err != nil {
		protoo.Close()
		return fmt.Errorf("%w: pion send: %v", call.ErrCannotJoinRoom, err)
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
		return fmt.Errorf("%w: SetRemoteDescription: %v", call.ErrCannotJoinRoom, err)
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
		return fmt.Errorf("%w: connectWebRtcTransport: %v", call.ErrCannotJoinRoom, err)
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
		return fmt.Errorf("%w: produce: %v", call.ErrCannotJoinRoom, err)
	}
	var result ms.ProduceResult
	json.Unmarshal(produceData, &result)
	l.Infof("producing [id:%s]", result.ID)

	// 9. Start streaming IVF frames.
	go ivf.LoopIntoTrack(l, track, p.cameras.NewSource())

	p.mu.Lock()
	p.protoos = append(p.protoos, protoo)
	p.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// viewer
// ---------------------------------------------------------------------------

func (p *Plugin) runViewer(ctx context.Context, l *log.Logger, roomID, userID string) error {
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
		return fmt.Errorf("%w: protoo: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("protoo connected")

	// 2. Get router capabilities.
	capsData, err := protooClient.Request("getRouterRtpCapabilities", nil)
	if err != nil {
		protooClient.Close()
		return fmt.Errorf("%w: getRouterRtpCapabilities: %v", call.ErrCannotJoinRoom, err)
	}
	caps, capsErr := unmarshalRouterCaps(capsData)
	if capsErr != nil {
		protooClient.Close()
		return fmt.Errorf("%w: %v", call.ErrCannotJoinRoom, capsErr)
	}

	// 3. Create recv transport.
	transportData, err := protooClient.Request("createWebRtcTransport", map[string]any{
		"forceTcp": false,
		"appData":  map[string]any{"direction": "consumer"},
	})
	if err != nil {
		protooClient.Close()
		return fmt.Errorf("%w: createWebRtcTransport: %v", call.ErrCannotJoinRoom, err)
	}
	json.Unmarshal(transportData, &transport)
	l.Infof("recv transport created: %s", transport.ID)

	// 4. Create recv PeerConnection.
	recvPC, err = ms.StartRecvPC(l, p.clientIP, p.vp9PT)
	if err != nil {
		protooClient.Close()
		return fmt.Errorf("%w: pion recv: %v", call.ErrCannotJoinRoom, err)
	}

	// 5. Set up track handler (fires after SDP negotiation adds consumer tracks).
	recvPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		l.Infof("[pion] OnTrack: %s %s PT=%d SSRC=%d", track.Kind(), track.Codec().MimeType, track.PayloadType(), track.SSRC())
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
				l.Infof("enabled IVF recording for room=%s user=%s", roomID, userID)
			}
		}
		if _, err := p.viewerManager.SpawnViewer(track, receiver, userID, cfg, seg); err != nil {
			l.Errorf("[viewer] SpawnViewer failed: %v", err)
		}
	})

	recvPC.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		l.Infof("[pion] ConnectionState: %s", s)
	})

	// 6. Join room — triggers newConsumer requests for existing producers.
	_, err = protooClient.Request("join", map[string]any{
		"displayName":     userID,
		"rtpCapabilities": caps,
		"device":          map[string]any{"name": "CallZIP", "flag": "go"},
	})
	if err != nil {
		protooClient.Close()
		return fmt.Errorf("%w: join: %v", call.ErrCannotJoinRoom, err)
	}
	l.Infof("joined room %s", roomID)

	p.mu.Lock()
	p.protoos = append(p.protoos, protooClient)
	p.mu.Unlock()

	return nil
}
