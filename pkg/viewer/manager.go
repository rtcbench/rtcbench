package viewer

import (
	"sync"

	"call.zip/pkg/log"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
	"github.com/pion/webrtc/v4"
)

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
