package metricsserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

func sampleAt(s *Server, at time.Time, name string, bitrate, fps float32, lost int64) {
	s.observe(at, vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:         name,
		SmoothBitrate:    bitrate,
		SmoothFPS:        fps,
		DecoderSmoothFPS: fps - 0.5,
		FrameJitterUS:    1000,
		FramesComplete:   100,
		FramesLost:       lost,
	})
}

func field(name string) int {
	for i, f := range SeriesFields {
		if f == name {
			return i
		}
	}
	panic(name)
}

func TestRecordingTicksAggregatesAndNotifies(t *testing.T) {
	s := New(0)
	start := time.Unix(1_700_000_000, 0)
	s.Record(start, Thresholds{})
	sub := s.Subscribe()
	defer s.Unsubscribe(sub)

	sampleAt(s, start.Add(200*time.Millisecond), "v1", 1_000_000, 24, 0)
	sampleAt(s, start.Add(300*time.Millisecond), "v2", 2_000_000, 30, 3)
	sampleAt(s, start.Add(400*time.Millisecond), "v3", 3_000_000, 20, 1)
	sampleAt(s, start.Add(500*time.Millisecond), "v3", 3_000_000, 22, 2)
	s.Tick(start.Add(999 * time.Millisecond))
	if len(sub.updates) != 0 {
		t.Fatal("ticked before the first interval")
	}
	s.Tick(start.Add(time.Second))

	var update Update
	select {
	case update = <-sub.Updates():
	default:
		t.Fatal("no update after the first tick")
	}
	p := update.Point
	if len(p) != len(SeriesFields) {
		t.Fatalf("point has %d fields, want %d", len(p), len(SeriesFields))
	}
	if p[field("t")] != float64(start.Add(time.Second).UnixMilli()) || p[field("viewers")] != 3 || p[field("active")] != 3 {
		t.Fatalf("point: %v", p)
	}
	if p[field("bitrate")] != 6_000_000 || p[field("fps_p50")] != 24 || p[field("fps_p10")] != 22 || p[field("fps_p90")] != 30 {
		t.Fatalf("aggregates: %v", p)
	}
	if p[field("frames_lost")] != 5 || p[field("frames_complete")] != 300 || p[field("dec_fps_p50")] != 23.5 {
		t.Fatalf("counters: %v", p)
	}
	if p[field("below")] != 0 || p[field("rtt_p50")] != 0 || p[field("layers_s0")] != 3 || p[field("layers_s2")] != 0 {
		t.Fatalf("thresholds, rtt and layers: %v", p)
	}
	if strings.Join(update.Names, ",") != "v1,v2,v3" || len(update.Rows) != 3 || update.Rows[2][0] != 2 {
		t.Fatalf("names %v rows %v", update.Names, update.Rows)
	}
	if row := update.Rows[2]; row[1] != 3_000_000 || row[2] != 22 || row[12] != 2 || row[13] != 0.5 || row[14] != 0 || len(row) != len(ViewerFields)+1 {
		t.Fatalf("v3 row: %v", row)
	}
	if len(update.Buckets) != 1 || update.Buckets[0].Index != 0 || len(update.Buckets[0].Rows) != 3 || update.Buckets[0].Rows[2] != [3]float64{2, 3_000_000, 21} {
		t.Fatalf("buckets: %+v", update.Buckets)
	}

	s.Tick(start.Add(2 * time.Second))
	update = <-sub.Updates()
	if update.Point[field("active")] != 3 || update.Rows != nil || update.Names != nil {
		t.Fatalf("quiet tick: %+v", update)
	}
	if len(update.Buckets) != 1 || update.Buckets[0].Index != 1 || update.Buckets[0].Rows != nil {
		t.Fatalf("quiet bucket: %+v", update.Buckets)
	}
	s.Tick(start.Add(40 * time.Second))
	snapshot := s.Snapshot()
	if p := snapshot.Series.Points[39]; p[field("viewers")] != 3 || p[field("active")] != 0 || p[field("bitrate")] != 0 || p[field("frames_lost")] != 5 {
		t.Fatalf("stale tick: %v", p)
	}
	if snapshot.Series.Interval != 1000 || len(snapshot.Series.Points) != 40 || snapshot.Series.Start != start.UnixMilli() {
		t.Fatalf("series: interval %d, %d points", snapshot.Series.Interval, len(snapshot.Series.Points))
	}
	if len(snapshot.Viewers.Names) != 3 || len(snapshot.Viewers.Rows) != 3 || snapshot.Viewers.Interval != 1000 {
		t.Fatalf("viewers: %+v", snapshot.Viewers)
	}
	h := snapshot.Viewers.History[2]
	if h.Offset != 0 || len(h.Values) != 40 || h.Values[0] != [2]float32{3_000_000, 21} || h.Values[39] != [2]float32{3_000_000, 21} {
		t.Fatalf("history: %+v", h)
	}
}

func TestRecordingCompactsSeriesAndHistories(t *testing.T) {
	s := New(0)
	start := time.Unix(1_700_000_000, 0)
	s.Record(start, Thresholds{})
	sub := s.Subscribe()
	defer s.Unsubscribe(sub)
	sampleAt(s, start.Add(500*time.Millisecond), "v1", 1_000_000, 24, 0)
	s.Tick(start.Add(5 * time.Second))
	sampleAt(s, start.Add(5500*time.Millisecond), "v2", 2_000_000, 30, 0)
	s.Tick(start.Add(seriesLimit * time.Second))

	series := s.Series()
	if series.Interval != 2000 || len(series.Points) != seriesLimit/2 {
		t.Fatalf("series: interval %d, %d points", series.Interval, len(series.Points))
	}
	if last := series.Points[len(series.Points)-1]; last[field("t")] != float64(start.Add(seriesLimit*time.Second).UnixMilli()) {
		t.Fatalf("last point: %v", last)
	}
	if first := series.Points[0]; first[field("t")] != float64(start.Add(2*time.Second).UnixMilli()) || first[field("bitrate")] != 1_000_000 {
		t.Fatalf("first point: %v", first)
	}
	viewers := s.Snapshot().Viewers
	if viewers.Interval != 16000 {
		t.Fatalf("history interval %d", viewers.Interval)
	}
	if h := viewers.History[0]; h.Offset != 0 || len(h.Values) != 56 {
		t.Fatalf("v1 history: offset %d, %d values", h.Offset, len(h.Values))
	}
	if h := viewers.History[1]; h.Offset != 0 || len(h.Values) != 56 || h.Values[0] != [2]float32{2_000_000, 30} {
		t.Fatalf("v2 history: offset %d, %d values, first %v", h.Offset, len(h.Values), h.Values[0])
	}
	drained := 0
	var resync bool
	for len(sub.updates) > 0 {
		update := <-sub.Updates()
		drained++
		if update.Series != nil && update.Viewers != nil {
			resync = true
		}
	}
	if drained == 0 || resync {
		t.Fatalf("drained %d updates, resync %v", drained, resync)
	}
	s.Tick(start.Add(seriesLimit*time.Second + 2*time.Second))
	if update := <-sub.Updates(); update.Series == nil || update.Viewers == nil || update.Point != nil {
		t.Fatalf("no resync after lag: %+v", update)
	}
	s.Tick(start.Add(seriesLimit*time.Second + 4*time.Second))
	if update := <-sub.Updates(); update.Point == nil || update.Series != nil {
		t.Fatalf("no plain update after resync: %+v", update)
	}
}

func TestDownsampleAndSelect(t *testing.T) {
	series := Series{Fields: SeriesFields, Interval: 1000, Start: 0}
	for i := range 10 {
		p := make([]float64, len(SeriesFields))
		p[field("t")] = float64(1000 * (i + 1))
		p[field("bitrate")] = float64(i)
		p[field("jitter_p90")] = float64(i)
		p[field("frames_lost")] = float64(i)
		series.Points = append(series.Points, p)
	}
	small := series.Downsample(4).Select("t", "bitrate", "jitter_p90", "frames_lost")
	if len(small.Points) != 4 || small.Interval != 3000 || len(small.Fields) != 4 {
		t.Fatalf("downsample: %+v", small)
	}
	if p := small.Points[0]; p[0] != 3000 || p[1] != 1 || p[2] != 2 || p[3] != 2 {
		t.Fatalf("first merged point: %v", p)
	}
	if p := small.Points[3]; p[0] != 10000 || p[1] != 9 {
		t.Fatalf("last point: %v", p)
	}
	if same := series.Downsample(10); len(same.Points) != 10 || same.Interval != 1000 {
		t.Fatalf("downsample within limit changed the series: %+v", same)
	}
}

func TestReplayRebuildsSeriesFromJSONL(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var lines []string
	line := func(offset float64, viewer string, bitrate, fps float64, lost int64, errs string) {
		lines = append(lines, fmt.Sprintf(`{"ts":%.3f,"client":"github.com/rtcbench/rtcbench","host":"h","instance":0,"viewer":%q,"bitrate_bps":%g,"fps":%g,"dec_sm_fps":%g,"dec_buf_fps":%g,"est_dec_fps":%d,"frame_jitter_us":1500,"packet_jitter_us":10,"frames_complete":50,"frames_lost":%d,"pli_sent":1,"max_recv_sid":2,"max_recv_tid":2%s}`,
			float64(start.Unix())+offset, viewer, bitrate, fps, fps, fps, int(fps), lost, errs))
	}
	line(2.5, "v1", 1_000_000, 25, 0, "")
	line(2.6, "v2", 3_000_000, 24, 1, "")
	lines = append(lines, "not json", "")
	line(5.5, "v1", 1_200_000, 26, 0, `,"errors":{"`+vp9_stats.ErrIDToString(0)+`":2}`)
	line(5.6, "v2", 3_000_000, 24, 4, "")
	s, err := Replay(strings.NewReader(strings.Join(lines, "\n")+"\n"), start)
	if err != nil {
		t.Fatal(err)
	}
	series := s.Series()
	if series.Start != start.UnixMilli() || series.Interval != 1000 || len(series.Points) != 6 {
		t.Fatalf("series: start %d interval %d points %d", series.Start, series.Interval, len(series.Points))
	}
	if p := series.Points[1]; p[field("viewers")] != 0 {
		t.Fatalf("point before the first sample: %v", p)
	}
	if p := series.Points[2]; p[field("viewers")] != 2 || p[field("bitrate")] != 4_000_000 || p[field("frames_lost")] != 1 {
		t.Fatalf("point at 3s: %v", p)
	}
	if p := series.Points[5]; p[field("bitrate")] != 4_200_000 || p[field("frames_lost")] != 4 || p[field("errors")] != 2 || p[field("pli")] != 2 {
		t.Fatalf("point at 6s: %v", p)
	}
	viewers := s.Snapshot().Viewers
	if strings.Join(viewers.Names, ",") != "v1,v2" || viewers.History[0].Offset != 2 || len(viewers.History[0].Values) != 4 {
		t.Fatalf("viewers: %+v", viewers)
	}
	if viewers.Errors[vp9_stats.ErrIDToString(0)] != 2 {
		t.Fatalf("errors: %v", viewers.Errors)
	}
	s, err = Replay(strings.NewReader(strings.Join(lines, "\n")), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if series := s.Series(); series.Start != start.Add(2500*time.Millisecond).UnixMilli() || len(series.Points) != 4 {
		t.Fatalf("series from first sample: start %d, %d points", series.Start, len(series.Points))
	}
	if _, err := Replay(strings.NewReader(""), start); err != nil {
		t.Fatal(err)
	}
}

func TestRecordingCountsThresholdsRTTAndLayers(t *testing.T) {
	s := New(0)
	start := time.Unix(1_700_000_000, 0)
	s.Record(start, Thresholds{MinFPS: 20, MaxRTTMS: 100})
	sub := s.Subscribe()
	defer s.Unsubscribe(sub)
	rtt := s.RTTSubscriber()
	at := start.Add(500 * time.Millisecond)
	s.observe(at, vp9_stats.Period{}, vp9_stats.VideoQualitySample{Nickname: "u1", SmoothFPS: 24, SmoothBitrate: 1e6, SVC: vp9_stats.SVCData{MaxRecvSID: 2}})
	s.observe(at, vp9_stats.Period{}, vp9_stats.VideoQualitySample{Nickname: "u2[cam]", SmoothFPS: 24, SmoothBitrate: 1e6, SVC: vp9_stats.SVCData{MaxRecvSID: 1}})
	s.observe(at, vp9_stats.Period{}, vp9_stats.VideoQualitySample{Nickname: "u3", SmoothFPS: 15, SmoothBitrate: 1e6, SVC: vp9_stats.SVCData{MaxRecvSID: 1}})
	rtt("u1", 30*time.Millisecond)
	rtt("u2", 250*time.Millisecond)
	rtt("missing", 10*time.Millisecond)
	rtt("u3", 0)
	s.Tick(start.Add(time.Second))
	update := <-sub.Updates()
	p := update.Point
	if p[field("below")] != 2 || p[field("rtt_p50")] != 30 || p[field("rtt_p90")] != 250 {
		t.Fatalf("thresholds and rtt: %v", p)
	}
	if p[field("layers_s0")] != 0 || p[field("layers_s1")] != 2 || p[field("layers_s2")] != 1 {
		t.Fatalf("layers: %v", p)
	}
	if len(update.Rows) != 3 || update.Rows[1][len(ViewerFields)] != 250 {
		t.Fatalf("rows: %v", update.Rows)
	}
	series := s.Series()
	if series.Thresholds == nil || series.Thresholds.MinFPS != 20 || series.Thresholds.MaxRTTMS != 100 || series.Thresholds.MinBitrateBps != 0 {
		t.Fatalf("thresholds: %+v", series.Thresholds)
	}
	if New(0).Series().Thresholds != nil {
		t.Fatal("thresholds reported without a recording")
	}
}

func TestFreshRecordingMarshalsEmptyPoints(t *testing.T) {
	s := New(0)
	s.Record(time.Unix(1_700_000_000, 0), Thresholds{})
	out, err := json.Marshal(s.Snapshot().Series)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"points":[]`) {
		t.Fatalf("fresh series marshals as %s", out)
	}
}
