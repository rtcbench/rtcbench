package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rtcbench/rtcbench/pkg/metricsserver"
	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

type liveEvent struct {
	Type    string                 `json:"type"`
	ID      string                 `json:"id"`
	Run     *Run                   `json:"run"`
	Point   []float64              `json:"p"`
	Rows    [][]float64            `json:"v"`
	Names   []string               `json:"n"`
	Series  json.RawMessage        `json:"series"`
	Viewers *metricsserver.Viewers `json:"viewers"`
}

func dialLive(t *testing.T, ctx context.Context, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader:      http.Header{"Origin": {strings.Replace(url[:strings.Index(url, "/api/")], "ws://", "http://", 1)}},
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func readLive(t *testing.T, ctx context.Context, conn *websocket.Conn) liveEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var event liveEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return event
}

func readLiveType(t *testing.T, ctx context.Context, conn *websocket.Conn, kind string) liveEvent {
	t.Helper()
	for range 20 {
		if event := readLive(t, ctx, conn); event.Type == kind {
			return event
		}
	}
	t.Fatalf("no %s message", kind)
	return liveEvent{}
}

func feed(s *metricsserver.Server, name string, bitrate float32, lost int64) {
	s.Subscriber()(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname: name, SmoothBitrate: bitrate, SmoothFPS: 25, DecoderSmoothFPS: 24, FramesComplete: 10, FramesLost: lost,
	})
}

func TestLiveRunAndRunsStreams(t *testing.T) {
	s, h := newServer(t)
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(server.URL, "http")

	list := dialLive(t, ctx, ws+"/api/v1/runs/live")
	defer list.CloseNow()
	if event := readLive(t, ctx, list); event.Type != "snapshot" || (len(event.Series) > 0 && string(event.Series) != "{}") {
		t.Fatalf("list snapshot: %+v", event)
	}

	if code := do(t, h, "POST", "/api/v1/environments", DocumentRequest{Name: "lab", YAML: labEnv}).Code; code != http.StatusCreated {
		t.Fatalf("env: %d", code)
	}
	thresholds := strings.Replace(config, "  logging:", "  metrics:\n    receiverThresholds:\n      - profile: clean\n        minFPS: 20\n      - profile: lossy\n        minFPS: 10\n        maxMeanRTTMS: 300\n  logging:", 1)
	rec := do(t, h, "POST", "/api/v1/runs", StartRequest{Name: "live", Config: thresholds, EnvName: "lab", StartAt: time.Now().Add(time.Hour).Unix()})
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var run Run
	_ = json.NewDecoder(rec.Body).Decode(&run)
	if event := readLive(t, ctx, list); event.Type != "run" || event.Run == nil || event.Run.ID != run.ID || event.Run.Status != StatusRunning {
		t.Fatalf("list run event: %+v", event)
	}
	s.mu.Lock()
	rn := s.runs[run.ID]
	s.mu.Unlock()

	page := dialLive(t, ctx, ws+"/api/v1/runs/"+run.ID+"/live")
	defer page.CloseNow()
	snapshot := readLive(t, ctx, page)
	if snapshot.Type != "snapshot" || snapshot.Run == nil || snapshot.Run.Status != StatusRunning || snapshot.Viewers == nil || len(snapshot.Viewers.Names) != 0 {
		t.Fatalf("page snapshot: %+v", snapshot)
	}
	var series metricsserver.Series
	if err := json.Unmarshal(snapshot.Series, &series); err != nil || series.Interval != 1000 || len(series.Fields) != len(metricsserver.SeriesFields) {
		t.Fatalf("page series: %s (%v)", snapshot.Series, err)
	}
	if series.Thresholds == nil || series.Thresholds.MinFPS != 10 || series.Thresholds.MaxRTTMS != 300 || series.Thresholds.MinBitrateBps != 0 {
		t.Fatalf("thresholds: %+v", series.Thresholds)
	}

	feed(rn.metrics, "viewer-001", 2_000_000, 1)
	feed(rn.metrics, "viewer-002", 3_000_000, 0)
	rn.metrics.RTTSubscriber()("viewer-002", 400*time.Millisecond)
	rn.metrics.Tick(time.Now().Add(2 * time.Second))
	tick := readLiveType(t, ctx, page, "tick")
	if len(tick.Point) != len(metricsserver.SeriesFields) || tick.Point[1] != 2 || tick.Point[3] != 5_000_000 || strings.Join(tick.Names, ",") != "viewer-001,viewer-002" || len(tick.Rows) != 2 {
		t.Fatalf("page tick: %+v", tick)
	}
	if tick.Point[14] != 1 || tick.Point[15] != 400 {
		t.Fatalf("below threshold and rtt: %v", tick.Point)
	}
	listTick := readLiveType(t, ctx, list, "tick")
	if listTick.ID != run.ID || len(listTick.Point) != len(sparklineFields) || listTick.Point[3] != 5_000_000 {
		t.Fatalf("list tick: %+v", listTick)
	}

	if err := page.Write(ctx, websocket.MessageText, []byte(`{"viewers":true}`)); err != nil {
		t.Fatal(err)
	}
	if viewers := readLiveType(t, ctx, page, "viewers"); viewers.Viewers == nil || len(viewers.Viewers.Rows) != 2 || viewers.Viewers.Rows[1][0] != 3_000_000 {
		t.Fatalf("viewers resync: %+v", viewers)
	}
	if err := page.Write(ctx, websocket.MessageText, []byte(`{"viewers":false}`)); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; ; attempt++ {
		feed(rn.metrics, "viewer-001", 2_500_000, 1)
		rn.metrics.Tick(time.Now().Add(time.Duration(2+attempt) * time.Second))
		if tick := readLiveType(t, ctx, page, "tick"); tick.Rows == nil {
			break
		}
		if attempt == 10 {
			t.Fatal("viewer rows kept coming after {\"viewers\":false}")
		}
	}

	if code := do(t, h, "POST", "/api/v1/runs/"+run.ID+"/stop", nil).Code; code != http.StatusAccepted {
		t.Fatalf("stop: %d", code)
	}
	final := readLiveType(t, ctx, page, "run")
	if final.Run == nil || final.Run.Status != StatusCompleted {
		t.Fatalf("final run: %+v", final)
	}
	if _, _, err := page.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("page stream did not close normally: %v", err)
	}
	if event := readLiveType(t, ctx, list, "run"); event.Run == nil || event.Run.Status != StatusCompleted {
		t.Fatalf("list final run: %+v", event)
	}
	dir := s.runDir(rn)
	if _, err := os.Stat(filepath.Join(dir, seriesFile)); err != nil {
		t.Fatal(err)
	}
	var saved metricsserver.Snapshot
	rec = do(t, h, "GET", "/api/v1/runs/"+run.ID+"/series", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); rec.Code != 200 || err != nil || len(saved.Series.Points) < 2 || saved.Series.Points[len(saved.Series.Points)-1][3] != 5_500_000 {
		t.Fatalf("saved series: %d %s", rec.Code, rec.Body)
	}
	if len(saved.Viewers.Names) != 2 || len(saved.Viewers.History) != 2 || len(saved.Viewers.History[0].Values) == 0 {
		t.Fatalf("saved viewers: %+v", saved.Viewers)
	}
	var sparklines map[string]metricsserver.Series
	rec = do(t, h, "GET", "/api/v1/runs/series", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &sparklines); err != nil || len(sparklines[run.ID].Points) < 2 || len(sparklines[run.ID].Fields) != len(sparklineFields) {
		t.Fatalf("sparklines: %s", rec.Body)
	}

	do(t, h, "PATCH", "/api/v1/runs/"+run.ID, UpdateRequest{Name: "renamed"})
	if event := readLiveType(t, ctx, list, "run"); event.Run == nil || event.Run.Name != "renamed" {
		t.Fatalf("renamed: %+v", event)
	}
	do(t, h, "DELETE", "/api/v1/runs/"+run.ID, nil)
	if event := readLiveType(t, ctx, list, "deleted"); event.ID != run.ID {
		t.Fatalf("deleted: %+v", event)
	}
	if code := do(t, h, "GET", "/api/v1/runs/"+run.ID+"/live", nil).Code; code != http.StatusNotFound {
		t.Fatalf("live for a deleted run: %d", code)
	}
}

func TestSeriesRebuildsFromStatsJSONL(t *testing.T) {
	s, h := newServer(t)
	if code := do(t, h, "POST", "/api/v1/environments", DocumentRequest{Name: "lab", YAML: labEnv}).Code; code != http.StatusCreated {
		t.Fatalf("env: %d", code)
	}
	rec := do(t, h, "POST", "/api/v1/runs", StartRequest{Name: "old", Config: config, EnvName: "lab"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var run Run
	_ = json.NewDecoder(rec.Body).Decode(&run)
	s.mu.Lock()
	rn := s.runs[run.ID]
	s.mu.Unlock()
	<-rn.done
	dir := s.runDir(rn)
	if err := os.Remove(filepath.Join(dir, seriesFile)); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	rn.sparkline = nil
	s.mu.Unlock()
	var lines []string
	for i := range 5 {
		ts := float64(run.StartedAt.UnixMicro())/1e6 + float64(i) + 0.5
		lines = append(lines, fmt.Sprintf(`{"ts":%.6f,"viewer":"viewer-001","bitrate_bps":1000000,"fps":25,"dec_sm_fps":25,"dec_buf_fps":25,"est_dec_fps":25,"frame_jitter_us":100,"frames_complete":%d,"frames_lost":0,"pli_sent":0,"max_recv_sid":1,"max_recv_tid":2}`, ts, 10*(i+1)))
	}
	if err := os.WriteFile(filepath.Join(dir, "bench-rtcbench-host-0.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var rebuilt metricsserver.Snapshot
	rec = do(t, h, "GET", "/api/v1/runs/"+run.ID+"/series", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &rebuilt); rec.Code != 200 || err != nil || len(rebuilt.Series.Points) != 5 || rebuilt.Series.Points[4][3] != 1_000_000 || rebuilt.Series.Points[4][10] != 50 {
		t.Fatalf("rebuilt series: %d %s", rec.Code, rec.Body)
	}
	if len(rebuilt.Viewers.Names) != 1 || len(rebuilt.Viewers.History[0].Values) != 5 {
		t.Fatalf("rebuilt viewers: %+v", rebuilt.Viewers)
	}
	if _, err := os.Stat(filepath.Join(dir, seriesFile)); err != nil {
		t.Fatal("rebuilt series was not saved")
	}
	rec = do(t, h, "GET", "/api/v1/runs/series", nil)
	var sparklines map[string]metricsserver.Series
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec = do(t, h, "GET", "/api/v1/runs/series", nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &sparklines)
		if len(sparklines[run.ID].Points) == 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sparklines: %s", rec.Body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := os.Remove(filepath.Join(dir, seriesFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "bench-rtcbench-host-0.jsonl")); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, "GET", "/api/v1/runs/"+run.ID+"/series", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &rebuilt); rec.Code != 200 || err != nil || len(rebuilt.Series.Points) != 0 || rebuilt.Series.Fields == nil || rebuilt.Viewers.Names == nil {
		t.Fatalf("empty series: %d %s", rec.Code, rec.Body)
	}
}

func TestLiveRejectsCrossOriginAndPlainRequests(t *testing.T) {
	s, h := newServer(t)
	s.settings.AllowedHosts = []string{"bench.example"}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for origin, want := range map[string]bool{
		"https://untrusted.example": false,
		"https://bench.example":     true,
		"http://bench.example:8080": true,
	} {
		conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/runs/live", &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": {origin}},
		})
		if conn != nil {
			conn.CloseNow()
		}
		if (err == nil) != want {
			t.Fatalf("origin %s accepted=%v (%v, %v)", origin, err == nil, response, err)
		}
	}
	if code := do(t, h, "GET", "/api/v1/runs/live", nil).Code; code != http.StatusBadRequest {
		t.Fatalf("plain request: %d", code)
	}
}
