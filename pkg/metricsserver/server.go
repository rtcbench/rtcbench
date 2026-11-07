package metricsserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

const activeViewerWindow = 30 * time.Second

type viewerState struct {
	Nickname            string
	SmoothBitrateBps    float32
	SmoothFPS           float32
	DecoderSmoothFPS    float32
	DecoderBufferFPS    float32
	EstimatedDecoderFPS int
	FrameJitterUS       float64
	FramesComplete      int64
	FramesLost          int64
	PLISent             int64
	MaxRecvSID          uint8
	MaxRecvTID          uint8
	SampleCount         int64
	LastSeenAt          time.Time
	RTTMS               float64

	index      int
	dirty      bool
	offset     int
	history    [][2]float32
	sumBitrate float64
	sumFPS     float64
	samples    int
}

// Server is a lightweight HTTP metrics server that tracks per-viewer stats.
type Server struct {
	port    int
	mu      sync.RWMutex
	viewers map[string]*viewerState
	order   []*viewerState
	errors  [vp9_stats.NumErrIDs]int32
	rec     *recording
}

// New creates a Server that will listen on the given port.
func New(port int) *Server {
	return &Server{
		port:    port,
		viewers: make(map[string]*viewerState),
	}
}

// Subscriber returns a func compatible with vp9_stats.Publisher.AddSubscriber.
func (s *Server) Subscriber() func(vp9_stats.Period, vp9_stats.VideoQualitySample) {
	return func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		s.observe(time.Now(), period, sample)
	}
}

func (s *Server) RTTSubscriber() func(userID string, rtt time.Duration) {
	return func(userID string, rtt time.Duration) {
		if rtt <= 0 {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, vs := range s.order {
			if vs.Nickname != userID && !strings.HasPrefix(vs.Nickname, userID+"[") {
				continue
			}
			vs.RTTMS = float64(rtt.Microseconds()) / 1000
			if s.rec != nil && !vs.dirty {
				vs.dirty = true
				s.rec.dirty = append(s.rec.dirty, vs)
			}
		}
	}
}

func (s *Server) observe(now time.Time, period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vs, ok := s.viewers[sample.Nickname]
	if !ok {
		vs = &viewerState{Nickname: sample.Nickname, index: len(s.order)}
		s.viewers[sample.Nickname] = vs
		s.order = append(s.order, vs)
		if s.rec != nil {
			s.rec.added = append(s.rec.added, vs)
		}
	}
	vs.SmoothBitrateBps = sample.SmoothBitrate
	vs.SmoothFPS = sample.SmoothFPS
	vs.DecoderSmoothFPS = sample.DecoderSmoothFPS
	vs.DecoderBufferFPS = sample.DecoderBufferFPS
	vs.EstimatedDecoderFPS = sample.EstimatedDecoderFPS
	vs.FrameJitterUS = sample.FrameJitterUS
	vs.FramesComplete = sample.FramesComplete
	vs.FramesLost = sample.FramesLost
	vs.PLISent = sample.RTCP.PLISent
	vs.MaxRecvSID = sample.SVC.MaxRecvSID
	vs.MaxRecvTID = sample.SVC.MaxRecvTID
	vs.SampleCount++
	vs.LastSeenAt = now
	s.errors = period.Errors
	if s.rec != nil {
		if !vs.dirty {
			vs.dirty = true
			s.rec.dirty = append(s.rec.dirty, vs)
		}
		vs.sumBitrate += float64(sample.SmoothBitrate)
		vs.sumFPS += float64(sample.SmoothFPS)
		vs.samples++
	}
}

// ListenAndServe starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handleHealth(w, r)
}

type healthResponse struct {
	Status               string           `json:"status"`
	ViewersTotal         int              `json:"viewers_total"`
	ViewersActive        int              `json:"viewers_active"`
	AggregateBitrateMbps float32          `json:"aggregate_bitrate_mbps"`
	Errors               map[string]int32 `json:"errors,omitempty"`
	Viewers              []viewerReport   `json:"viewers"`
}

type viewerReport struct {
	Nickname            string  `json:"nickname"`
	SmoothBitrateBps    float32 `json:"smooth_bitrate_bps"`
	SmoothFPS           float32 `json:"smooth_fps"`
	DecoderSmoothFPS    float32 `json:"dec_sm_fps"`
	DecoderBufferFPS    float32 `json:"dec_buf_fps"`
	EstimatedDecoderFPS int     `json:"est_dec_fps"`
	FrameJitterUS       float64 `json:"frame_jitter_us"`
	FramesComplete      int64   `json:"frames_complete"`
	FramesLost          int64   `json:"frames_lost"`
	PLISent             int64   `json:"pli_sent"`
	MaxRecvSID          uint8   `json:"max_recv_sid"`
	MaxRecvTID          uint8   `json:"max_recv_tid"`
	SampleCount         int64   `json:"sample_count"`
	LastSeenAgoMs       int64   `json:"last_seen_ago_ms"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	s.mu.RLock()
	defer s.mu.RUnlock()

	reports := make([]viewerReport, 0, len(s.viewers))
	var totalBitrate float32
	activeCount := 0

	for _, vs := range s.order {
		agoMs := now.Sub(vs.LastSeenAt).Milliseconds()
		reports = append(reports, viewerReport{
			Nickname:            vs.Nickname,
			SmoothBitrateBps:    vs.SmoothBitrateBps,
			SmoothFPS:           vs.SmoothFPS,
			DecoderSmoothFPS:    vs.DecoderSmoothFPS,
			DecoderBufferFPS:    vs.DecoderBufferFPS,
			EstimatedDecoderFPS: vs.EstimatedDecoderFPS,
			FrameJitterUS:       vs.FrameJitterUS,
			FramesComplete:      vs.FramesComplete,
			FramesLost:          vs.FramesLost,
			PLISent:             vs.PLISent,
			MaxRecvSID:          vs.MaxRecvSID,
			MaxRecvTID:          vs.MaxRecvTID,
			SampleCount:         vs.SampleCount,
			LastSeenAgoMs:       agoMs,
		})
		if now.Sub(vs.LastSeenAt) <= activeViewerWindow {
			activeCount++
			totalBitrate += vs.SmoothBitrateBps
		}
	}

	status := "no_data"
	if activeCount > 0 && totalBitrate > 0 {
		status = "ok"
	}

	resp := healthResponse{
		Status:               status,
		ViewersTotal:         len(s.viewers),
		ViewersActive:        activeCount,
		AggregateBitrateMbps: totalBitrate / 1_000_000,
		Errors:               errorCounts(s.errors),
		Viewers:              reports,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
