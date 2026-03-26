package vp9_stats

import (
	"sync"
	"sync/atomic"
)

// RTCPTracker tracks RTCP metrics for a viewer; safe for concurrent use.
type RTCPTracker struct {
	pliSent atomic.Int64
	srCount atomic.Int64
}

// IncrPLI records that a PLI was sent.
func (t *RTCPTracker) IncrPLI() {
	t.pliSent.Add(1)
}

// IncrSR records that a Sender Report was received.
func (t *RTCPTracker) IncrSR() {
	t.srCount.Add(1)
}

// Snapshot returns the current RTCP counters.
func (t *RTCPTracker) Snapshot() RTCPData {
	return RTCPData{
		PLISent: t.pliSent.Load(),
		SRCount: t.srCount.Load(),
	}
}

// RTCPData carries RTCP feedback stats through the pipeline.
type RTCPData struct {
	PLISent int64 `json:"pli_sent,omitempty"`
	SRCount int64 `json:"sr_count,omitempty"`
}

// PLIThrottle rate-limits PLI sending to avoid flooding.
type PLIThrottle struct {
	mu       sync.Mutex
	sendPLI  func()
	tracker  *RTCPTracker
	minNanos int64
	lastSend int64 // UnixNano of last PLI send
}

// NewPLIThrottle wraps sendPLI with rate limiting. minInterval is the minimum
// nanoseconds between PLI sends (e.g., 100ms = 100_000_000).
func NewPLIThrottle(sendPLI func(), tracker *RTCPTracker, minIntervalNanos int64) *PLIThrottle {
	return &PLIThrottle{
		sendPLI:  sendPLI,
		tracker:  tracker,
		minNanos: minIntervalNanos,
	}
}

// OnFrameLost is the callback to pass to FrameStatistics. It sends a PLI
// if enough time has elapsed since the last one.
func (p *PLIThrottle) OnFrameLost(nowNano int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if nowNano-p.lastSend < p.minNanos {
		return
	}
	p.lastSend = nowNano
	p.sendPLI()
	p.tracker.IncrPLI()
}
