package viewer

import (
	"fmt"
	"path"
	"sync"

	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/vp9"
	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
	"github.com/pion/webrtc/v4"
)

// TrackHandlerOpts configures HandleTrack. Each plugin provides these values
// from its own config and transport layer.
type TrackHandlerOpts struct {
	StatsBufferSize    int
	PacketCaptureDir   string
	EnableRecording    bool
	RecordingDirectory string
	PacketsPerSample   int // 0 defaults to DefaultPacketsPerSample
	RoomID             string
	UserID             string
	Logger             *log.Logger
	SendPLI            func() // plugin-specific PLI sender
}

// HandleTrack is the shared OnTrack handler. It sets up RTCP PLI throttling,
// builds a viewer Config, optionally creates an IvfSegmenter for recording, and
// spawns a Viewer. This replaces ~35 near-identical lines in each plugin.
func (m *Manager) HandleTrack(
	track *webrtc.TrackRemote,
	receiver *webrtc.RTPReceiver,
	pub *vp9_stats.Publisher,
	opts TrackHandlerOpts,
) (*Viewer, error) {
	l := opts.Logger

	rtcpTracker := &vp9_stats.RTCPTracker{}
	throttle := vp9_stats.NewPLIThrottle(opts.SendPLI, rtcpTracker, DefaultPLIMinIntervalNS)

	pps := opts.PacketsPerSample
	if pps == 0 {
		pps = DefaultPacketsPerSample
	}

	cfg := &Config{
		PacketsPerSample:  pps,
		VP9RTPPayloadType: int(track.PayloadType()),
		TrackBufferSize:   DefaultTrackBufferSize,
		StatsBufferSize:   opts.StatsBufferSize,
		PacketCaptureDir:  opts.PacketCaptureDir,
	}

	var seg *vp9.IvfSegmenter
	if opts.EnableRecording {
		recDir := path.Join(opts.RecordingDirectory, fmt.Sprintf("/room=%s/user=%s", opts.RoomID, opts.UserID))
		var recErr error
		seg, recErr = vp9.NewIvfSegmenter(recDir)
		if recErr != nil {
			l.Errorf("[viewer] IvfSegmenter failed: %v", recErr)
		} else {
			seg.Enable()
			l.Infof("enabled IVF file writing for room=%s user=%s", opts.RoomID, opts.UserID)
		}
	}

	v, err := m.SpawnViewer(track, receiver, opts.UserID, cfg, seg, pub, rtcpTracker, throttle.OnFrameLost)
	if err != nil {
		l.Errorf("[viewer] SpawnViewer failed: %v", err)
		return nil, err
	}
	return v, nil
}

type Manager struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	once    sync.Once
	done    chan struct{}
	viewers map[*Viewer]struct{}
	input   chan<- vp9_stats.VideoQualitySample
	log     *log.Logger
}

func NewManager(input chan<- vp9_stats.VideoQualitySample, log *log.Logger) *Manager {
	return &Manager{
		done:    make(chan struct{}),
		viewers: make(map[*Viewer]struct{}),
		input:   input,
		log:     log,
	}
}

func (m *Manager) SpawnViewer(
	track *webrtc.TrackRemote,
	receiver *webrtc.RTPReceiver,
	nickname string,
	config *Config,
	ivf *vp9.IvfSegmenter,
	pub *vp9_stats.Publisher,
	rtcpTracker *vp9_stats.RTCPTracker,
	onFrameLost func(nowNano int64),
) (*Viewer, error) {
	v, err := newViewer(track, receiver, m.input, nickname, config, ivf, pub, rtcpTracker, onFrameLost)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.viewers[v] = struct{}{}
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := v.run(m.done)

		if err != nil {
			m.log.Errorf("viewer exited with error(s): %v", err)
		} else {
			m.log.Infof("viewer exited normally")
		}

		m.mu.Lock()
		delete(m.viewers, v)
		m.mu.Unlock()
	}()
	go func() {
		<-m.done
		v.stop()
	}()

	return v, nil
}

func (m *Manager) StopAll() {
	m.once.Do(func() {
		close(m.done)
	})
	m.wg.Wait()
}

func (m *Manager) Size() (n int) {
	m.mu.Lock()
	n = len(m.viewers)
	m.mu.Unlock()
	return
}
