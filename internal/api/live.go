package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rtcbench/rtcbench/internal/core"
	"github.com/rtcbench/rtcbench/pkg/metricsserver"
)

const (
	seriesFile       = "timeseries.json"
	sparklinePoints  = 60
	liveReadLimit    = 4 << 10
	livePing         = 30 * time.Second
	liveWriteTimeout = 10 * time.Second
	liveBuffer       = 64
)

var sparklineFields = []string{"t", "viewers", "active", "bitrate", "fps_p50", "frames_complete", "frames_lost", "below"}

func liveThresholds(configured []core.ReceiverThresholdConfig) metricsserver.Thresholds {
	var t metricsserver.Thresholds
	for _, c := range configured {
		if c.MinFPS != nil && *c.MinFPS > 0 && (t.MinFPS == 0 || *c.MinFPS < t.MinFPS) {
			t.MinFPS = *c.MinFPS
		}
		if c.MinBitrateBps != nil && *c.MinBitrateBps > 0 && (t.MinBitrateBps == 0 || *c.MinBitrateBps < t.MinBitrateBps) {
			t.MinBitrateBps = *c.MinBitrateBps
		}
		if c.MaxJitterUS != nil && *c.MaxJitterUS > 0 && *c.MaxJitterUS > t.MaxJitterUS {
			t.MaxJitterUS = *c.MaxJitterUS
		}
		if c.MaxMeanRTTMS != nil && *c.MaxMeanRTTMS > 0 && *c.MaxMeanRTTMS > t.MaxRTTMS {
			t.MaxRTTMS = *c.MaxMeanRTTMS
		}
	}
	return t
}

type liveState struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	subs   map[*liveClient]struct{}
	build  sync.Once
}

type liveClient struct {
	messages chan []byte
}

type listMessage struct {
	Type   string                          `json:"type"`
	ID     string                          `json:"id,omitempty"`
	Run    *Run                            `json:"run,omitempty"`
	Point  []float64                       `json:"p,omitempty"`
	Series map[string]metricsserver.Series `json:"series,omitempty"`
}

type runMessage struct {
	Type string `json:"type"`
	Run  *Run   `json:"run,omitempty"`
	metricsserver.Update
}

func newLive() *liveState {
	ctx, cancel := context.WithCancel(context.Background())
	return &liveState{ctx: ctx, cancel: cancel, subs: map[*liveClient]struct{}{}}
}

func (l *liveState) add() *liveClient {
	client := &liveClient{messages: make(chan []byte, liveBuffer)}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subs[client] = struct{}{}
	return client
}

func (l *liveState) remove(client *liveClient) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.subs, client)
}

func (l *liveState) broadcast(message listMessage) {
	data, err := json.Marshal(message)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for client := range l.subs {
		select {
		case client.messages <- data:
		default:
			delete(l.subs, client)
			close(client.messages)
		}
	}
}

func (l *liveState) run(run Run) {
	l.broadcast(listMessage{Type: "run", Run: &run})
}

func (l *liveState) deleted(id string) {
	l.broadcast(listMessage{Type: "deleted", ID: id})
}

func (s *Server) liveOptions() *websocket.AcceptOptions {
	patterns := make([]string, 0, 2*len(s.settings.AllowedHosts))
	for _, host := range s.settings.AllowedHosts {
		patterns = append(patterns, host, host+":*")
	}
	return &websocket.AcceptOptions{
		OriginPatterns:  patterns,
		CompressionMode: websocket.CompressionContextTakeover,
	}
}

func (s *Server) acceptLive(w http.ResponseWriter, r *http.Request) (*websocket.Conn, bool) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		writeError(w, http.StatusBadRequest, errors.New("websocket upgrade required"))
		return nil, false
	}
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		writeError(w, http.StatusServiceUnavailable, errors.New("server is shutting down"))
		return nil, false
	}
	conn, err := websocket.Accept(w, r, s.liveOptions())
	if err != nil {
		return nil, false
	}
	return conn, true
}

func writeLive(ctx context.Context, conn *websocket.Conn, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, liveWriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}

func writeLiveJSON(ctx context.Context, conn *websocket.Conn, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return writeLive(ctx, conn, data)
}

func pingLive(ctx context.Context, conn *websocket.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, liveWriteTimeout)
	defer cancel()
	return conn.Ping(ctx)
}

func (s *Server) liveRuns(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.acceptLive(w, r)
	if !ok {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(liveReadLimit)
	client := s.live.add()
	defer s.live.remove(client)
	ctx := conn.CloseRead(s.live.ctx)
	if err := writeLiveJSON(ctx, conn, listMessage{Type: "snapshot", Series: s.sparklines()}); err != nil {
		return
	}
	ping := time.NewTicker(livePing)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-client.messages:
			if !ok {
				conn.Close(websocket.StatusTryAgainLater, "too slow")
				return
			}
			if err := writeLive(ctx, conn, data); err != nil {
				return
			}
		case <-ping.C:
			if err := pingLive(ctx, conn); err != nil {
				return
			}
		}
	}
}

func (s *Server) liveRun(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	run, live, done := rn.Run, rn.Status == StatusRunning && rn.metrics != nil, rn.done
	s.mu.Unlock()
	if !live {
		writeError(w, http.StatusConflict, errors.New("run is not running"))
		return
	}
	conn, ok := s.acceptLive(w, r)
	if !ok {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(liveReadLimit)
	ctx, cancel := context.WithCancel(s.live.ctx)
	defer cancel()
	control := make(chan bool, 1)
	go func() {
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var message struct {
				Viewers *bool `json:"viewers"`
			}
			if json.Unmarshal(data, &message) == nil && message.Viewers != nil {
				select {
				case control <- *message.Viewers:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	sub := rn.metrics.Subscribe()
	defer rn.metrics.Unsubscribe(sub)
	snapshot := rn.metrics.Snapshot()
	err := writeLiveJSON(ctx, conn, runMessage{
		Type:   "snapshot",
		Run:    &run,
		Update: metricsserver.Update{Series: &snapshot.Series, Viewers: &snapshot.Viewers},
	})
	if err != nil {
		return
	}
	sent := lastTime(snapshot.Series)
	stale := func(update metricsserver.Update) bool {
		if update.Point != nil {
			return update.Point[0] <= sent
		}
		return update.Series != nil && lastTime(*update.Series) <= sent
	}
	viewers := true
	ping := time.NewTicker(livePing)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			s.mu.Lock()
			run = rn.Run
			s.mu.Unlock()
			if writeLiveJSON(ctx, conn, runMessage{Type: "run", Run: &run}) == nil {
				conn.Close(websocket.StatusNormalClosure, "run ended")
			}
			return
		case update := <-sub.Updates():
			if stale(update) {
				continue
			}
			if !viewers {
				update.Rows, update.Names, update.Buckets, update.Viewers = nil, nil, nil, nil
			}
			if err := writeLiveJSON(ctx, conn, runMessage{Type: "tick", Update: update}); err != nil {
				return
			}
		case want := <-control:
			viewers = want
			if want {
				current := rn.metrics.Snapshot()
				sent = lastTime(current.Series)
				if err := writeLiveJSON(ctx, conn, runMessage{Type: "viewers", Update: metricsserver.Update{Viewers: &current.Viewers}}); err != nil {
					return
				}
			}
		case <-ping.C:
			if err := pingLive(ctx, conn); err != nil {
				return
			}
		}
	}
}

func (s *Server) forwardTicks(ctx context.Context, rn *run) {
	sub := rn.metrics.Subscribe()
	defer rn.metrics.Unsubscribe(sub)
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-sub.Updates():
			if update.Series != nil {
				series := sparkline(*update.Series)
				s.live.broadcast(listMessage{Type: "series", Series: map[string]metricsserver.Series{rn.ID: series}})
			} else if update.Point != nil {
				point := metricsserver.Series{Fields: metricsserver.SeriesFields, Points: [][]float64{update.Point}}.Select(sparklineFields...)
				s.live.broadcast(listMessage{Type: "tick", ID: rn.ID, Point: point.Points[0]})
			}
		}
	}
}

func lastTime(series metricsserver.Series) float64 {
	if n := len(series.Points); n > 0 {
		return series.Points[n-1][0]
	}
	return 0
}

func sparkline(series metricsserver.Series) metricsserver.Series {
	return series.Select(sparklineFields...).Downsample(sparklinePoints)
}

func (s *Server) getSeries(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	live := rn.Status == StatusRunning && rn.metrics != nil
	s.mu.Unlock()
	if live {
		writeJSON(w, http.StatusOK, rn.metrics.Snapshot())
		return
	}
	snapshot, err := s.loadSnapshot(rn)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) listSeries(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.sparklines())
}

func (s *Server) sparklines() map[string]metricsserver.Series {
	s.mu.Lock()
	defer s.mu.Unlock()
	series := make(map[string]metricsserver.Series, len(s.runs))
	pending := false
	for id, rn := range s.runs {
		switch {
		case rn.Status == StatusRunning && rn.metrics != nil:
			series[id] = sparkline(rn.metrics.Series())
		case rn.sparkline != nil:
			series[id] = *rn.sparkline
		default:
			pending = true
		}
	}
	if pending {
		s.live.build.Do(func() { go s.buildSparklines() })
	}
	return series
}

func (s *Server) buildSparklines() {
	s.mu.Lock()
	var pending []*run
	for _, rn := range s.runs {
		if rn.Status != StatusRunning && rn.sparkline == nil {
			pending = append(pending, rn)
		}
	}
	s.mu.Unlock()
	for _, rn := range pending {
		if s.live.ctx.Err() != nil {
			return
		}
		snapshot, err := s.loadSnapshot(rn)
		if err != nil {
			s.Logf("run %s series: %v", rn.ID, err)
			snapshot = emptySnapshot()
		}
		spark := sparkline(snapshot.Series)
		s.mu.Lock()
		current := s.runs[rn.ID] == rn
		if current {
			rn.sparkline = &spark
		}
		s.mu.Unlock()
		if current {
			s.live.broadcast(listMessage{Type: "series", Series: map[string]metricsserver.Series{rn.ID: spark}})
		}
	}
}

func (s *Server) loadSnapshot(rn *run) (metricsserver.Snapshot, error) {
	rn.seriesMu.Lock()
	defer rn.seriesMu.Unlock()
	s.mu.Lock()
	dir, started := rn.dir, rn.StartedAt
	s.mu.Unlock()
	path := filepath.Join(dir, seriesFile)
	var snapshot metricsserver.Snapshot
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &snapshot); err == nil && snapshot.Series.Fields != nil {
			if snapshot.Series.Points == nil {
				snapshot.Series.Points = [][]float64{}
			}
			return snapshot, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return snapshot, err
	}
	snapshot, err = rebuildSnapshot(dir, started)
	if err != nil {
		return snapshot, err
	}
	saveSnapshot(dir, snapshot)
	return snapshot, nil
}

func emptySnapshot() metricsserver.Snapshot {
	return metricsserver.New(0).Snapshot()
}

func rebuildSnapshot(dir string, started time.Time) (metricsserver.Snapshot, error) {
	matches, _ := filepath.Glob(filepath.Join(dir, "bench-rtcbench-*.jsonl"))
	if len(matches) == 0 {
		return emptySnapshot(), nil
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return emptySnapshot(), err
	}
	defer f.Close()
	ms, err := metricsserver.Replay(f, started)
	if err != nil {
		return emptySnapshot(), err
	}
	return ms.Snapshot(), nil
}

func saveSnapshot(dir string, snapshot metricsserver.Snapshot) {
	if data, err := json.Marshal(snapshot); err == nil {
		_ = os.WriteFile(filepath.Join(dir, seriesFile), data, 0o644)
	}
}
