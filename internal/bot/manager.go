package bot

import (
	"log"
	"sync"

	"call.zip/internal/vp9_stats"
	"github.com/pion/webrtc/v3"
)

type Manager struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	once    sync.Once
	done    chan struct{}
	viewers map[*Viewer]struct{}
}

func NewManager() *Manager {
	return &Manager{
		done:    make(chan struct{}),
		viewers: make(map[*Viewer]struct{}),
	}
}

func (m *Manager) SpawnViewer(
	track *webrtc.TrackRemote,
	receiver *webrtc.RTPReceiver,
	onSample func(*vp9_stats.VideoQualitySample),
	config *ViewerConfig,
) (*Viewer, error) {
	v, err := newViewer(track, receiver, config)

	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.viewers[v] = struct{}{}
	m.mu.Unlock()

	m.wg.Add(1)
	go func(onSample_ func(*vp9_stats.VideoQualitySample)) {
		defer m.wg.Done()
		err := v.run(m.done, onSample_)

		// TODO do something with the error...
		if err != nil {
			log.Printf("Viewer exited with error(s): %v", err)
		} else {
			log.Println("Viewer exited normally")
		}

		m.mu.Lock()
		delete(m.viewers, v)
		m.mu.Unlock()
	}(onSample)
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
