package metricsserver

import (
	"context"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

const (
	seriesLimit      = 900
	historyLimit     = 60
	initialInterval  = time.Second
	subscriberBuffer = 16
)

var SeriesFields = []string{
	"t", "viewers", "active", "bitrate",
	"fps_p10", "fps_p50", "fps_p90", "dec_fps_p50",
	"jitter_p50", "jitter_p90",
	"frames_complete", "frames_lost", "pli", "errors",
	"below", "rtt_p50", "rtt_p90", "layers_s0", "layers_s1", "layers_s2",
}

var ViewerFields = []string{
	"bitrate", "fps", "dec_fps", "buf_fps", "est_dec_fps", "jitter",
	"frames_complete", "frames_lost", "pli", "sid", "tid", "samples", "seen",
	"rtt",
}

type Thresholds struct {
	MinFPS        float64 `json:"min_fps,omitempty"`
	MinBitrateBps float64 `json:"min_bitrate_bps,omitempty"`
	MaxJitterUS   float64 `json:"max_jitter_us,omitempty"`
	MaxRTTMS      float64 `json:"max_rtt_ms,omitempty"`
}

func (t Thresholds) empty() bool {
	return t == Thresholds{}
}

func (t Thresholds) below(vs *viewerState) bool {
	return (t.MinFPS > 0 && float64(vs.SmoothFPS) < t.MinFPS) ||
		(t.MinBitrateBps > 0 && float64(vs.SmoothBitrateBps) < t.MinBitrateBps) ||
		(t.MaxJitterUS > 0 && vs.FrameJitterUS > t.MaxJitterUS) ||
		(t.MaxRTTMS > 0 && vs.RTTMS > t.MaxRTTMS)
}

var HistoryFields = []string{"bitrate", "fps"}

type merge uint8

const (
	mergeLast merge = iota
	mergeMean
	mergeMax
)

var seriesMerges = []merge{
	mergeLast, mergeLast, mergeLast, mergeMean,
	mergeMean, mergeMean, mergeMean, mergeMean,
	mergeMean, mergeMax,
	mergeLast, mergeLast, mergeLast, mergeLast,
	mergeLast, mergeMean, mergeMax, mergeLast, mergeLast, mergeLast,
}

type Series struct {
	Start      int64       `json:"start"`
	Interval   int64       `json:"interval"`
	Fields     []string    `json:"fields"`
	Points     [][]float64 `json:"points"`
	Thresholds *Thresholds `json:"thresholds,omitempty"`
}

type History struct {
	Offset int          `json:"o"`
	Values [][2]float32 `json:"h"`
}

type Viewers struct {
	Fields   []string         `json:"fields"`
	Names    []string         `json:"names"`
	Rows     [][]float64      `json:"rows"`
	Start    int64            `json:"start"`
	Interval int64            `json:"interval"`
	History  []History        `json:"history"`
	Errors   map[string]int32 `json:"errors,omitempty"`
}

type Snapshot struct {
	Series  Series  `json:"series"`
	Viewers Viewers `json:"viewers"`
}

type Bucket struct {
	Index int          `json:"i"`
	Rows  [][3]float64 `json:"r,omitempty"`
}

type Update struct {
	Point   []float64        `json:"p,omitempty"`
	Rows    [][]float64      `json:"v,omitempty"`
	Names   []string         `json:"n,omitempty"`
	Buckets []Bucket         `json:"b,omitempty"`
	Errors  map[string]int32 `json:"e,omitempty"`
	Series  *Series          `json:"series,omitempty"`
	Viewers *Viewers         `json:"viewers,omitempty"`
}

type Subscriber struct {
	updates chan Update
	lagged  bool
}

func (sub *Subscriber) Updates() <-chan Update {
	return sub.updates
}

type recording struct {
	thresholds Thresholds
	start      time.Time
	interval   time.Duration
	next       time.Time
	points     [][]float64
	hInterval  time.Duration
	hNext      time.Time
	hBuckets   int
	dirty      []*viewerState
	added      []*viewerState
	errors     [vp9_stats.NumErrIDs]int32
	subs       map[*Subscriber]struct{}
}

func (s *Server) Record(start time.Time, thresholds Thresholds) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec = &recording{
		thresholds: thresholds,
		start:      start,
		interval:   initialInterval,
		next:       start.Add(initialInterval),
		hInterval:  initialInterval,
		hNext:      start.Add(initialInterval),
		subs:       map[*Subscriber]struct{}{},
	}
}

func (s *Server) Run(ctx context.Context) {
	for {
		s.mu.RLock()
		rec := s.rec
		var wait time.Duration
		if rec != nil {
			wait = time.Until(rec.next)
		}
		s.mu.RUnlock()
		if rec == nil {
			return
		}
		timer := time.NewTimer(max(wait, 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.Tick(time.Now())
		}
	}
}

func (s *Server) Tick(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rec
	if rec == nil {
		return
	}
	for !rec.next.After(now) {
		s.step(rec.next)
	}
}

func (s *Server) step(at time.Time) {
	rec := s.rec
	update := Update{Point: s.point(at)}
	rec.points = append(rec.points, update.Point)
	if len(rec.points) >= seriesLimit {
		rec.points = compactPoints(rec.points)
		rec.interval *= 2
		series := s.seriesLocked()
		update.Series, update.Point = &series, nil
	}
	rec.next = rec.next.Add(rec.interval)

	compactedHistory := false
	for !rec.hNext.After(at) {
		bucket := Bucket{Index: rec.hBuckets}
		for _, vs := range s.order {
			if value, fresh := vs.closeBucket(rec.hBuckets); fresh {
				bucket.Rows = append(bucket.Rows, [3]float64{float64(vs.index), float64(value[0]), float64(value[1])})
			}
		}
		update.Buckets = append(update.Buckets, bucket)
		rec.hBuckets++
		if rec.hBuckets >= historyLimit {
			for _, vs := range s.order {
				vs.compactHistory()
			}
			rec.hBuckets /= 2
			rec.hInterval *= 2
			compactedHistory = true
		}
		rec.hNext = rec.hNext.Add(rec.hInterval)
	}
	if compactedHistory {
		viewers := s.viewersLocked(at)
		update.Viewers, update.Buckets = &viewers, nil
	} else {
		for _, vs := range rec.added {
			update.Names = append(update.Names, vs.Nickname)
		}
		for _, vs := range rec.dirty {
			update.Rows = append(update.Rows, append([]float64{float64(vs.index)}, vs.row(at)...))
		}
		if rec.errors != s.errors {
			update.Errors = errorCounts(s.errors)
			if update.Errors == nil {
				update.Errors = map[string]int32{}
			}
		}
	}
	rec.errors = s.errors
	for _, vs := range rec.dirty {
		vs.dirty = false
	}
	rec.dirty, rec.added = rec.dirty[:0], rec.added[:0]

	for sub := range rec.subs {
		if sub.lagged {
			if len(sub.updates) > 0 {
				continue
			}
			series, viewers := s.seriesLocked(), s.viewersLocked(at)
			sub.updates <- Update{Series: &series, Viewers: &viewers}
			sub.lagged = false
			continue
		}
		select {
		case sub.updates <- update:
		default:
			sub.lagged = true
		}
	}
}

func (s *Server) Subscribe() *Subscriber {
	sub := &Subscriber{updates: make(chan Update, subscriberBuffer)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec != nil {
		s.rec.subs[sub] = struct{}{}
	}
	return sub
}

func (s *Server) Unsubscribe(sub *Subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec != nil {
		delete(s.rec.subs, sub)
	}
}

func (s *Server) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Snapshot{Series: s.seriesLocked(), Viewers: s.viewersLocked(time.Now())}
}

func (s *Server) Series() Series {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.seriesLocked()
}

func (s *Server) seriesLocked() Series {
	series := Series{Fields: SeriesFields, Points: [][]float64{}}
	if s.rec != nil {
		series.Start = s.rec.start.UnixMilli()
		series.Interval = s.rec.interval.Milliseconds()
		series.Points = append(series.Points, s.rec.points...)
		if !s.rec.thresholds.empty() {
			thresholds := s.rec.thresholds
			series.Thresholds = &thresholds
		}
	}
	return series
}

func (s *Server) viewersLocked(now time.Time) Viewers {
	viewers := Viewers{
		Fields:  ViewerFields,
		Names:   make([]string, 0, len(s.order)),
		Rows:    make([][]float64, 0, len(s.order)),
		History: make([]History, 0, len(s.order)),
		Errors:  errorCounts(s.errors),
	}
	if s.rec != nil {
		viewers.Start = s.rec.start.UnixMilli()
		viewers.Interval = s.rec.hInterval.Milliseconds()
	}
	for _, vs := range s.order {
		viewers.Names = append(viewers.Names, vs.Nickname)
		viewers.Rows = append(viewers.Rows, vs.row(now))
		viewers.History = append(viewers.History, History{Offset: vs.offset, Values: slices.Clone(vs.history)})
	}
	return viewers
}

func (s *Server) point(at time.Time) []float64 {
	var active, below int
	var bitrate float64
	var complete, lost, pli int64
	var layers [3]int
	fps := make([]float64, 0, len(s.order))
	dec := make([]float64, 0, len(s.order))
	jitter := make([]float64, 0, len(s.order))
	rtt := make([]float64, 0, len(s.order))
	thresholds := s.rec.thresholds
	for _, vs := range s.order {
		complete += vs.FramesComplete
		lost += vs.FramesLost
		pli += vs.PLISent
		if at.Sub(vs.LastSeenAt) <= activeViewerWindow {
			active++
			bitrate += float64(vs.SmoothBitrateBps)
			fps = append(fps, float64(vs.SmoothFPS))
			dec = append(dec, float64(vs.DecoderSmoothFPS))
			jitter = append(jitter, vs.FrameJitterUS)
			if vs.RTTMS > 0 {
				rtt = append(rtt, vs.RTTMS)
			}
			if thresholds.below(vs) {
				below++
			}
			layers[min(int(vs.MaxRecvSID), 2)]++
		}
	}
	var errors float64
	for _, count := range s.errors {
		errors += float64(count)
	}
	return []float64{
		float64(at.UnixMilli()), float64(len(s.order)), float64(active), math.Round(bitrate),
		quantile(fps, 0.1), quantile(fps, 0.5), quantile(fps, 0.9), quantile(dec, 0.5),
		quantile(jitter, 0.5), quantile(jitter, 0.9),
		float64(complete), float64(lost), float64(pli), errors,
		float64(below), quantile(rtt, 0.5), quantile(rtt, 0.9),
		float64(layers[0]), float64(layers[1]), float64(layers[2]),
	}
}

func (vs *viewerState) row(now time.Time) []float64 {
	return []float64{
		math.Round(float64(vs.SmoothBitrateBps)), round2(float64(vs.SmoothFPS)),
		round2(float64(vs.DecoderSmoothFPS)), round2(float64(vs.DecoderBufferFPS)),
		float64(vs.EstimatedDecoderFPS), math.Round(vs.FrameJitterUS),
		float64(vs.FramesComplete), float64(vs.FramesLost), float64(vs.PLISent),
		float64(vs.MaxRecvSID), float64(vs.MaxRecvTID), float64(vs.SampleCount),
		math.Round(now.Sub(vs.LastSeenAt).Seconds()*10) / 10,
		round2(vs.RTTMS),
	}
}

func (vs *viewerState) closeBucket(bucket int) ([2]float32, bool) {
	if vs.samples == 0 && len(vs.history) == 0 {
		return [2]float32{}, false
	}
	if len(vs.history) == 0 {
		vs.offset = bucket
	}
	fresh := vs.samples > 0
	var next [2]float32
	if fresh {
		next = [2]float32{
			float32(math.Round(vs.sumBitrate / float64(vs.samples))),
			float32(round2(vs.sumFPS / float64(vs.samples))),
		}
	} else {
		next = vs.history[len(vs.history)-1]
	}
	vs.history = append(vs.history, next)
	vs.sumBitrate, vs.sumFPS, vs.samples = 0, 0, 0
	return next, fresh
}

func (vs *viewerState) compactHistory() {
	if len(vs.history) == 0 {
		return
	}
	merged := make([][2]float32, 0, len(vs.history)/2+1)
	i := 0
	if vs.offset%2 == 1 {
		merged = append(merged, vs.history[0])
		i = 1
	}
	for ; i+1 < len(vs.history); i += 2 {
		a, b := vs.history[i], vs.history[i+1]
		merged = append(merged, [2]float32{
			float32(math.Round(float64(a[0]+b[0]) / 2)),
			float32(round2(float64(a[1]+b[1]) / 2)),
		})
	}
	if i < len(vs.history) {
		merged = append(merged, vs.history[i])
	}
	vs.offset /= 2
	vs.history = merged
}

func mergePoints(points [][]float64) []float64 {
	out := make([]float64, len(points[0]))
	for i := range out {
		switch seriesMerges[i] {
		case mergeMean:
			var sum float64
			for _, p := range points {
				sum += p[i]
			}
			out[i] = round2(sum / float64(len(points)))
		case mergeMax:
			for _, p := range points {
				out[i] = math.Max(out[i], p[i])
			}
		default:
			out[i] = points[len(points)-1][i]
		}
	}
	return out
}

func compactPoints(points [][]float64) [][]float64 {
	return downsample(points, (len(points)+1)/2)
}

func downsample(points [][]float64, limit int) [][]float64 {
	if limit <= 0 || len(points) <= limit {
		return points
	}
	size := (len(points) + limit - 1) / limit
	out := make([][]float64, 0, limit)
	for i := 0; i < len(points); i += size {
		out = append(out, mergePoints(points[i:min(i+size, len(points))]))
	}
	return out
}

func (series Series) Downsample(limit int) Series {
	if limit > 0 && len(series.Points) > limit {
		size := (len(series.Points) + limit - 1) / limit
		series.Points = downsample(series.Points, limit)
		series.Interval *= int64(size)
	}
	return series
}

func (series Series) Select(fields ...string) Series {
	indexes := make([]int, 0, len(fields))
	for _, field := range fields {
		if i := slices.Index(series.Fields, field); i >= 0 {
			indexes = append(indexes, i)
		}
	}
	points := make([][]float64, len(series.Points))
	for i, point := range series.Points {
		points[i] = make([]float64, len(indexes))
		for j, index := range indexes {
			points[i][j] = point[index]
		}
	}
	series.Fields, series.Points = fields, points
	return series
}

func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	sort.Float64s(sorted)
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	return round2(sorted[min(max(rank, 0), len(sorted)-1)])
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func errorCounts(errors [vp9_stats.NumErrIDs]int32) map[string]int32 {
	var counts map[string]int32
	for i, count := range errors {
		if count > 0 {
			if counts == nil {
				counts = make(map[string]int32)
			}
			counts[vp9_stats.ErrIDToString(i)] = count
		}
	}
	return counts
}
