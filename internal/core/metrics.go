package core

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

const (
	metricOperationTotal    = "rtcbench_operation_total"
	metricOperationDuration = "rtcbench_operation_duration_seconds"
	metricUsersActive       = "rtcbench_users_active"
	metricConnectionsJoined = "rtcbench_connections_joined"
	metricPublishersActive  = "rtcbench_publishers_active"
	metricRoomsActive       = "rtcbench_rooms_active"
	metricSessionDuration   = "rtcbench_session_duration_seconds"

	metricReceiverBitrateBps     = "rtcbench_receiver_bitrate_bps"
	metricReceiverFPS            = "rtcbench_receiver_fps"
	metricReceiverFreezeTotal    = "rtcbench_receiver_freeze_total"
	metricReceiverFrameLossRatio = "rtcbench_receiver_frame_loss_ratio"
	metricReceiverJitterUS       = "rtcbench_receiver_jitter_us"
	metricRTCPPliTotal           = "rtcbench_rtcp_pli_total"
	metricRTCPNackTotal          = "rtcbench_rtcp_nack_total"
	metricRTCPFirTotal           = "rtcbench_rtcp_fir_total"

	metricLabelScenario  = "scenario"
	metricLabelPlugin    = "plugin"
	metricLabelRole      = "role"
	metricLabelOp        = "op"
	metricLabelOutcome   = "outcome"
	metricLabelProfile   = "profile"
	metricLabelUser      = "user_id"
	metricOutcomeAttempt = "attempt"
	metricOutcomeSuccess = "success"
	metricOutcomeFailure = "failure"
	metricLabelAll       = "all"
	metricLabelUnknown   = "unknown"
	manualScenarioLabel  = "manual"
)

var histogramBucketBounds = []float64{
	0.001,
	0.0025,
	0.005,
	0.01,
	0.025,
	0.05,
	0.1,
	0.25,
	0.5,
	1,
	2,
	5,
	10,
	30,
	60,
}

type Labels map[string]string

type MetricsCollector interface {
	IncCounter(name string, value int64, labels Labels)
	ObserveHistogram(name string, value float64, labels Labels)
	SetGauge(name string, value float64, labels Labels)
}

type HistogramSnapshot struct {
	Count        int64
	Sum          float64
	Min          float64
	Max          float64
	BucketBounds []float64
	BucketCounts []uint64
}

func (h HistogramSnapshot) Mean() float64 {
	if h.Count == 0 {
		return 0
	}
	return h.Sum / float64(h.Count)
}

func (h HistogramSnapshot) Quantile(q float64) float64 {
	if h.Count == 0 {
		return 0
	}
	if q <= 0 {
		return h.Min
	}
	if q >= 1 {
		return h.Max
	}
	target := uint64(math.Ceil(q * float64(h.Count)))
	if target == 0 {
		target = 1
	}
	var cumulative uint64
	for index, count := range h.BucketCounts {
		cumulative += count
		if cumulative >= target {
			if index < len(h.BucketBounds) {
				return h.BucketBounds[index]
			}
			return h.Max
		}
	}
	return h.Max
}

type RunMetricsSnapshot struct {
	Counters   map[string]int64
	Gauges     map[string]float64
	GaugePeaks map[string]float64
	Histograms map[string]HistogramSnapshot
	Receivers  []ReceiverSummary
}

func (s RunMetricsSnapshot) CounterValue(name string, labels Labels) int64 {
	return s.Counters[metricSeriesKey(name, labels)]
}

func (s RunMetricsSnapshot) GaugeValue(name string, labels Labels) float64 {
	return s.Gauges[metricSeriesKey(name, labels)]
}

func (s RunMetricsSnapshot) GaugePeakValue(name string, labels Labels) float64 {
	return s.GaugePeaks[metricSeriesKey(name, labels)]
}

func (s RunMetricsSnapshot) Histogram(name string, labels Labels) HistogramSnapshot {
	return s.Histograms[metricSeriesKey(name, labels)]
}

type RunMetricsSummary struct {
	Operations []OperationSummary
	Sessions   []HistogramSummary
	Gauges     []GaugeSummary
	Receivers  []ReceiverSummary
}

type OperationSummary struct {
	Scenario    string
	Plugin      string
	Role        string
	Operation   string
	Attempts    int64
	Successes   int64
	Failures    int64
	FailureRate float64
	MeanSeconds float64
	P50Seconds  float64
	P95Seconds  float64
	P99Seconds  float64
}

type HistogramSummary struct {
	Scenario    string
	Plugin      string
	Role        string
	Name        string
	Count       int64
	MeanSeconds float64
	MinSeconds  float64
	MaxSeconds  float64
	P50Seconds  float64
	P95Seconds  float64
	P99Seconds  float64
}

type GaugeSummary struct {
	Scenario string
	Plugin   string
	Role     string
	Name     string
	Profile  string
	UserID   string
	Current  float64
	Peak     float64
}

type ReceiverSummary struct {
	Scenario            string
	Plugin              string
	Role                string
	Profile             string
	UserID              string
	FreezeCount         int64
	FreezeDurationTotal float64
	FrameLossRatio      float64
	MeanBitrateBps      float64
	MeanFPS             float64
	MaxJitterUS         float64
	MeanRTTMS           float64
	MaxRTTMS            float64
	PliCount            int64
	NackCount           int64
}

func (s RunMetricsSnapshot) Summary() RunMetricsSummary {
	operations := make(map[string]*OperationSummary)
	sessions := make([]HistogramSummary, 0)
	gauges := make([]GaugeSummary, 0)

	for key, value := range s.Counters {
		name, labels := parseMetricSeriesKey(key)
		if name != metricOperationTotal {
			continue
		}
		op := labels[metricLabelOp]
		groupKey := strings.Join([]string{
			labels[metricLabelScenario],
			labels[metricLabelPlugin],
			labels[metricLabelRole],
			op,
		}, "\x00")
		summary, exists := operations[groupKey]
		if !exists {
			summary = &OperationSummary{
				Scenario:  labels[metricLabelScenario],
				Plugin:    labels[metricLabelPlugin],
				Role:      labels[metricLabelRole],
				Operation: op,
			}
			operations[groupKey] = summary
		}
		switch labels[metricLabelOutcome] {
		case metricOutcomeAttempt:
			summary.Attempts = value
		case metricOutcomeSuccess:
			summary.Successes = value
		case metricOutcomeFailure:
			summary.Failures = value
		}
	}

	for key, histogram := range s.Histograms {
		name, labels := parseMetricSeriesKey(key)
		switch name {
		case metricOperationDuration:
			groupKey := strings.Join([]string{
				labels[metricLabelScenario],
				labels[metricLabelPlugin],
				labels[metricLabelRole],
				labels[metricLabelOp],
			}, "\x00")
			summary, exists := operations[groupKey]
			if !exists {
				summary = &OperationSummary{
					Scenario:  labels[metricLabelScenario],
					Plugin:    labels[metricLabelPlugin],
					Role:      labels[metricLabelRole],
					Operation: labels[metricLabelOp],
				}
				operations[groupKey] = summary
			}
			summary.MeanSeconds = histogram.Mean()
			summary.P50Seconds = histogram.Quantile(0.50)
			summary.P95Seconds = histogram.Quantile(0.95)
			summary.P99Seconds = histogram.Quantile(0.99)
		case metricSessionDuration:
			sessions = append(sessions, HistogramSummary{
				Scenario:    labels[metricLabelScenario],
				Plugin:      labels[metricLabelPlugin],
				Role:        labels[metricLabelRole],
				Name:        name,
				Count:       histogram.Count,
				MeanSeconds: histogram.Mean(),
				MinSeconds:  histogram.Min,
				MaxSeconds:  histogram.Max,
				P50Seconds:  histogram.Quantile(0.50),
				P95Seconds:  histogram.Quantile(0.95),
				P99Seconds:  histogram.Quantile(0.99),
			})
		}
	}

	result := RunMetricsSummary{
		Operations: make([]OperationSummary, 0, len(operations)),
		Sessions:   sessions,
		Gauges:     gauges,
		Receivers:  s.Receivers,
	}
	for _, summary := range operations {
		if summary.Attempts > 0 {
			summary.FailureRate = float64(summary.Failures) / float64(summary.Attempts)
		}
		result.Operations = append(result.Operations, *summary)
	}

	for key, current := range s.Gauges {
		name, labels := parseMetricSeriesKey(key)
		switch name {
		case metricUsersActive, metricConnectionsJoined, metricPublishersActive, metricRoomsActive:
			result.Gauges = append(result.Gauges, GaugeSummary{
				Scenario: labels[metricLabelScenario],
				Plugin:   labels[metricLabelPlugin],
				Role:     labels[metricLabelRole],
				Name:     name,
				Current:  current,
				Peak:     s.GaugePeaks[key],
			})
		}
	}

	sort.Slice(result.Operations, func(i, j int) bool {
		return operationSummarySortKey(result.Operations[i]) < operationSummarySortKey(result.Operations[j])
	})
	sort.Slice(result.Sessions, func(i, j int) bool {
		return histogramSummarySortKey(result.Sessions[i]) < histogramSummarySortKey(result.Sessions[j])
	})
	sort.Slice(result.Gauges, func(i, j int) bool {
		return gaugeSummarySortKey(result.Gauges[i]) < gaugeSummarySortKey(result.Gauges[j])
	})

	return result
}

type histogramSeries struct {
	counts []uint64
	count  int64
	sum    float64
	min    float64
	max    float64
}

type runMetricsCollector struct {
	mu sync.RWMutex

	counters   map[string]int64
	gauges     map[string]float64
	gaugePeaks map[string]float64
	histograms map[string]*histogramSeries

	userPluginConnections map[string]int64
	roomMembers           map[string]int64

	receiverTemplates map[string]receiverTemplate
	receivers         map[string]*receiverMetricsCollector
	archivedReceivers []ReceiverSummary
}

type receiverTemplate struct {
	scenario string
	plugin   string
	profile  string
}

func newRunMetricsCollector() *runMetricsCollector {
	return &runMetricsCollector{
		counters:              make(map[string]int64),
		gauges:                make(map[string]float64),
		gaugePeaks:            make(map[string]float64),
		histograms:            make(map[string]*histogramSeries),
		userPluginConnections: make(map[string]int64),
		roomMembers:           make(map[string]int64),
		receiverTemplates:     make(map[string]receiverTemplate),
		receivers:             make(map[string]*receiverMetricsCollector),
	}
}

func (m *runMetricsCollector) RegisterReceiver(userID, plugin, scenario, profile string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receiverTemplates[userID] = receiverTemplate{
		scenario: scenario,
		plugin:   plugin,
		profile:  profile,
	}
}

func (m *runMetricsCollector) UnregisterReceiver(userID string) ReceiverSummary {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.receiverTemplates, userID)

	var (
		summary ReceiverSummary
		found   bool
	)
	for key, r := range m.receivers {
		if !receiverBelongsToUser(key, userID) {
			continue
		}
		delete(m.receivers, key)
		current := r.Summary()
		m.archivedReceivers = append(m.archivedReceivers, current)
		if !found || key == userID {
			summary = current
			found = true
		}
	}
	if !found {
		return ReceiverSummary{}
	}
	return summary
}

func (m *runMetricsCollector) ObserveReceiverSample(userID string, sample vp9_stats.VideoQualitySample) {
	m.mu.Lock()
	r := m.ensureReceiverLocked(userID)
	m.mu.Unlock()
	if r == nil {
		return
	}
	r.ObserveSample(&sample)
}

func (m *runMetricsCollector) ObserveReceiverRTT(userID string, rtt time.Duration) {
	if rtt <= 0 {
		return
	}

	m.mu.RLock()
	receivers := make([]*receiverMetricsCollector, 0, len(m.receivers))
	for key, r := range m.receivers {
		if receiverBelongsToUser(key, userID) {
			receivers = append(receivers, r)
		}
	}
	m.mu.RUnlock()

	for _, r := range receivers {
		r.ObserveRTT(rtt)
	}
}

func (m *runMetricsCollector) ensureReceiverLocked(userID string) *receiverMetricsCollector {
	if r, ok := m.receivers[userID]; ok {
		return r
	}

	template, ok := m.lookupReceiverTemplateLocked(userID)
	if !ok {
		return nil
	}

	r := newReceiverMetrics(template.scenario, template.plugin, template.profile, userID)
	m.receivers[userID] = r

	return r
}

func (m *runMetricsCollector) lookupReceiverTemplateLocked(userID string) (receiverTemplate, bool) {
	if template, ok := m.receiverTemplates[userID]; ok {
		return template, true
	}

	baseUserID := baseReceiverUserID(userID)
	if baseUserID == userID {
		return receiverTemplate{}, false
	}

	template, ok := m.receiverTemplates[baseUserID]
	return template, ok
}

func (m *runMetricsCollector) Snapshot() RunMetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counters := make(map[string]int64, len(m.counters))
	for key, value := range m.counters {
		counters[key] = value
	}

	gauges := make(map[string]float64, len(m.gauges))
	for key, value := range m.gauges {
		gauges[key] = value
	}

	gaugePeaks := make(map[string]float64, len(m.gaugePeaks))
	for key, value := range m.gaugePeaks {
		gaugePeaks[key] = value
	}

	histograms := make(map[string]HistogramSnapshot, len(m.histograms))
	for key, series := range m.histograms {
		bucketCounts := make([]uint64, len(series.counts))
		copy(bucketCounts, series.counts[:])
		histograms[key] = HistogramSnapshot{
			Count:        series.count,
			Sum:          series.sum,
			Min:          series.min,
			Max:          series.max,
			BucketBounds: append([]float64(nil), histogramBucketBounds...),
			BucketCounts: bucketCounts,
		}
	}

	receivers := make([]ReceiverSummary, 0, len(m.receivers)+len(m.archivedReceivers))
	receivers = append(receivers, m.archivedReceivers...)
	for _, r := range m.receivers {
		receivers = append(receivers, r.Summary())
	}
	sort.Slice(receivers, func(i, j int) bool {
		return receivers[i].UserID < receivers[j].UserID
	})

	return RunMetricsSnapshot{
		Counters:   counters,
		Gauges:     gauges,
		GaugePeaks: gaugePeaks,
		Histograms: histograms,
		Receivers:  receivers,
	}
}

func (m *runMetricsCollector) IncCounter(name string, value int64, labels Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[metricSeriesKey(name, labels)] += value
}

func (m *runMetricsCollector) ObserveHistogram(name string, value float64, labels Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := metricSeriesKey(name, labels)
	series, exists := m.histograms[key]
	if !exists {
		series = &histogramSeries{
			counts: make([]uint64, len(histogramBucketBounds)+1),
			min:    value,
			max:    value,
		}
		m.histograms[key] = series
	}
	series.count++
	series.sum += value
	if value < series.min {
		series.min = value
	}
	if value > series.max {
		series.max = value
	}
	index := sort.SearchFloat64s(histogramBucketBounds, value)
	series.counts[index]++
}

func (m *runMetricsCollector) SetGauge(name string, value float64, labels Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[metricSeriesKey(name, labels)] = value
}

func (m *runMetricsCollector) RecordOperationAttempt(labels Labels, op string) {
	labeled := withLabel(labels, metricLabelOp, op)
	labeled = withLabel(labeled, metricLabelOutcome, metricOutcomeAttempt)
	m.IncCounter(metricOperationTotal, 1, labeled)
}

func (m *runMetricsCollector) RecordOperationResult(labels Labels, op string, durationSeconds float64, err error) {
	outcome := metricOutcomeSuccess
	if err != nil {
		outcome = metricOutcomeFailure
	}
	m.ObserveHistogram(metricOperationDuration, durationSeconds, withLabel(labels, metricLabelOp, op))
	m.IncCounter(metricOperationTotal, 1, withLabel(withLabel(labels, metricLabelOp, op), metricLabelOutcome, outcome))
}

func (m *runMetricsCollector) ObserveSessionDuration(labels Labels, durationSeconds float64) {
	m.ObserveHistogram(metricSessionDuration, durationSeconds, labels)
}

func (m *runMetricsCollector) ConnectionJoined(userID, roomID string, labels Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()

	roleLabels := subsetLabels(labels, metricLabelScenario, metricLabelPlugin, metricLabelRole)
	pluginLabels := subsetLabels(labels, metricLabelScenario, metricLabelPlugin)

	m.addGaugeLocked(metricConnectionsJoined, roleLabels, 1)

	userKey := stateKey(labels[metricLabelScenario], labels[metricLabelPlugin], labels[metricLabelRole], userID)
	m.userPluginConnections[userKey]++
	if m.userPluginConnections[userKey] == 1 {
		m.addGaugeLocked(metricUsersActive, roleLabels, 1)
	}

	roomKey := stateKey(labels[metricLabelScenario], labels[metricLabelPlugin], roomID)
	m.roomMembers[roomKey]++
	if m.roomMembers[roomKey] == 1 {
		m.addGaugeLocked(metricRoomsActive, pluginLabels, 1)
	}
}

func (m *runMetricsCollector) ConnectionLeft(userID, roomID string, labels Labels, wasPublished bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	roleLabels := subsetLabels(labels, metricLabelScenario, metricLabelPlugin, metricLabelRole)
	pluginLabels := subsetLabels(labels, metricLabelScenario, metricLabelPlugin)

	m.addGaugeLocked(metricConnectionsJoined, roleLabels, -1)

	userKey := stateKey(labels[metricLabelScenario], labels[metricLabelPlugin], labels[metricLabelRole], userID)
	if current := m.userPluginConnections[userKey]; current > 0 {
		current--
		if current == 0 {
			delete(m.userPluginConnections, userKey)
			m.addGaugeLocked(metricUsersActive, roleLabels, -1)
		} else {
			m.userPluginConnections[userKey] = current
		}
	}

	roomKey := stateKey(labels[metricLabelScenario], labels[metricLabelPlugin], roomID)
	if current := m.roomMembers[roomKey]; current > 0 {
		current--
		if current == 0 {
			delete(m.roomMembers, roomKey)
			m.addGaugeLocked(metricRoomsActive, pluginLabels, -1)
		} else {
			m.roomMembers[roomKey] = current
		}
	}

	if wasPublished {
		m.addGaugeLocked(metricPublishersActive, pluginLabels, -1)
	}
}

func (m *runMetricsCollector) VideoPublished(labels Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addGaugeLocked(metricPublishersActive, subsetLabels(labels, metricLabelScenario, metricLabelPlugin), 1)
}

func (m *runMetricsCollector) VideoUnpublished(labels Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addGaugeLocked(metricPublishersActive, subsetLabels(labels, metricLabelScenario, metricLabelPlugin), -1)
}

func (m *runMetricsCollector) addGaugeLocked(name string, labels Labels, delta float64) {
	key := metricSeriesKey(name, labels)
	next := m.gauges[key] + delta
	if next < 0 {
		next = 0
	}
	m.gauges[key] = next
	if next > m.gaugePeaks[key] {
		m.gaugePeaks[key] = next
	}
}

func metricSeriesKey(name string, labels Labels) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(name)
	for _, key := range keys {
		b.WriteByte(0)
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(labels[key])
	}
	return b.String()
}

func withLabel(labels Labels, key, value string) Labels {
	next := make(Labels, len(labels)+1)
	for existingKey, existingValue := range labels {
		next[existingKey] = existingValue
	}
	next[key] = value
	return next
}

func subsetLabels(labels Labels, keys ...string) Labels {
	next := make(Labels, len(keys))
	for _, key := range keys {
		next[key] = labels[key]
	}
	return next
}

func stateKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func parseMetricSeriesKey(key string) (string, Labels) {
	parts := strings.Split(key, "\x00")
	labels := make(Labels, max(len(parts)-1, 0))
	if len(parts) == 0 {
		return "", labels
	}
	for _, part := range parts[1:] {
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		labels[name] = value
	}
	return parts[0], labels
}

type FreezeDetector struct {
	mu                sync.Mutex
	fpsThreshold      float32
	freezeThresholdMS int
	lastFrameTime     time.Time
	freezeStart       time.Time
	isFreezing        bool
	totalFreezes      int64
	totalDuration     time.Duration
}

func NewFreezeDetector(fpsThreshold float32, freezeThresholdMS int) *FreezeDetector {
	return &FreezeDetector{
		fpsThreshold:      fpsThreshold,
		freezeThresholdMS: freezeThresholdMS,
	}
}

func (d *FreezeDetector) Update(now time.Time, fps float32) (inFreeze bool, freezeEnded bool, duration time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if fps < d.fpsThreshold {
		if !d.isFreezing {
			d.isFreezing = true
			d.freezeStart = now
		}
		return true, false, 0
	}

	if d.isFreezing {
		d.isFreezing = false
		d.totalFreezes++
		dur := now.Sub(d.freezeStart)
		d.totalDuration += dur
		return false, true, dur
	}

	return false, false, 0
}

func (d *FreezeDetector) Stats() (count int64, duration time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.totalFreezes, d.totalDuration
}

func (d *FreezeDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.totalFreezes = 0
	d.totalDuration = 0
	d.isFreezing = false
}

type receiverMetricsCollector struct {
	mu          sync.Mutex
	scenario    string
	plugin      string
	profile     string
	userID      string
	freeze      *FreezeDetector
	bitrateSum  float64
	bitrateCnt  int64
	fpsSum      float64
	fpsCnt      int64
	jitterMax   float64
	rttSumMS    float64
	rttCnt      int64
	rttMaxMS    float64
	pliCount    int64
	nackCount   int64
	framesTotal int64
	framesLost  int64
	initialized bool
}

func newReceiverMetrics(scenario, plugin, profile, userID string) *receiverMetricsCollector {
	return &receiverMetricsCollector{
		scenario:    scenario,
		plugin:      plugin,
		profile:     profile,
		userID:      userID,
		freeze:      NewFreezeDetector(5.0, 200),
		initialized: true,
	}
}

func (r *receiverMetricsCollector) ObserveSample(sample *vp9_stats.VideoQualitySample) {
	if sample == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.bitrateSum += float64(sample.SmoothBitrate)
	r.bitrateCnt++

	fps := float64(sample.DecoderSmoothFPS)
	r.fpsSum += fps
	r.fpsCnt++

	if sample.FrameJitterUS > r.jitterMax {
		r.jitterMax = sample.FrameJitterUS
	}

	if sample.RTCP.PLISent > r.pliCount {
		r.pliCount = sample.RTCP.PLISent
	}
	if sample.FramesComplete+sample.FramesLost > r.framesTotal {
		r.framesTotal = sample.FramesComplete + sample.FramesLost
	}
	if sample.FramesLost > r.framesLost {
		r.framesLost = sample.FramesLost
	}

	r.freeze.Update(time.Now(), float32(fps))
}

func (r *receiverMetricsCollector) ObserveRTT(rtt time.Duration) {
	if rtt <= 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	rttMS := float64(rtt) / float64(time.Millisecond)
	r.rttSumMS += rttMS
	r.rttCnt++
	if rttMS > r.rttMaxMS {
		r.rttMaxMS = rttMS
	}
}

func (r *receiverMetricsCollector) Summary() ReceiverSummary {
	r.mu.Lock()
	defer r.mu.Unlock()

	var bitrateMean, fpsMean, rttMeanMS float64
	if r.bitrateCnt > 0 {
		bitrateMean = r.bitrateSum / float64(r.bitrateCnt)
	}
	if r.fpsCnt > 0 {
		fpsMean = r.fpsSum / float64(r.fpsCnt)
	}
	if r.rttCnt > 0 {
		rttMeanMS = r.rttSumMS / float64(r.rttCnt)
	}

	var frameLossRatio float64
	if r.framesTotal > 0 {
		frameLossRatio = float64(r.framesLost) / float64(r.framesTotal)
	}

	freezeCount, freezeDur := r.freeze.Stats()

	return ReceiverSummary{
		Scenario:            r.scenario,
		Plugin:              r.plugin,
		Role:                string(Viewer),
		Profile:             r.profile,
		UserID:              r.userID,
		FreezeCount:         freezeCount,
		FreezeDurationTotal: freezeDur.Seconds(),
		FrameLossRatio:      frameLossRatio,
		MeanBitrateBps:      bitrateMean,
		MeanFPS:             fpsMean,
		MaxJitterUS:         r.jitterMax,
		MeanRTTMS:           rttMeanMS,
		MaxRTTMS:            r.rttMaxMS,
		PliCount:            r.pliCount,
		NackCount:           r.nackCount,
	}
}

func baseReceiverUserID(userID string) string {
	index := strings.LastIndexByte(userID, '[')
	if index <= 0 || !strings.HasSuffix(userID, "]") {
		return userID
	}
	return userID[:index]
}

func receiverBelongsToUser(candidate, userID string) bool {
	return candidate == userID || baseReceiverUserID(candidate) == userID
}

func operationSummarySortKey(summary OperationSummary) string {
	return strings.Join([]string{summary.Scenario, summary.Plugin, summary.Role, summary.Operation}, "\x00")
}

func histogramSummarySortKey(summary HistogramSummary) string {
	return strings.Join([]string{summary.Name, summary.Scenario, summary.Plugin, summary.Role}, "\x00")
}

func gaugeSummarySortKey(summary GaugeSummary) string {
	return strings.Join([]string{summary.Name, summary.Scenario, summary.Plugin, summary.Role}, "\x00")
}
