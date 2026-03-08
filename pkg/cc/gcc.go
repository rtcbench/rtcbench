package cc

import (
	"sync"
	"time"

	pionrtcp "github.com/pion/rtcp"
)

// Constants for the GCC controller.
const (
	gccMinBps     uint64  = 100_000    // 100 kbps floor
	gccMaxBps     uint64  = 10_000_000 // 10 Mbps ceiling
	gccInitialBps uint64  = 300_000    // 300 kbps start

	gccIncreaseStep   uint64  = 500_000 // +500 kbps per probe interval
	gccDecreaseFactor float64 = 0.85    // ×0.85 on overuse or high loss
	gccIncreaseDelay          = 200 * time.Millisecond

	// Overuse: current mean recv delta > EMA × overuseThreshold for
	// overuseConsecutive consecutive TWCC reports.
	gccOveruseThreshold   = 1.25
	gccOveruseConsecutive = 2

	// Loss threshold: >10 % loss triggers a multiplicative decrease.
	gccLossThreshold = 0.10

	// EMA smoothing factor for recv-delta baseline (closer to 1 = slower decay).
	gccEMAAlpha = 0.90
)

// GCC implements a simplified Google Congestion Control algorithm.
//
// It uses TWCC receive-delta trends to detect queue build-up and applies AIMD
// rate control: additive increase when the network is clear, multiplicative
// decrease when overuse or high loss is detected.  REMB is treated as a hard cap.
//
// Limitation: without per-packet send timestamps (not exposed by pion's
// interceptor API at this layer), the implementation compares the per-report
// mean receive delta against a smoothed baseline rather than the canonical
// one-way delay gradient.  This is a good approximation for a stress tester.
type GCC struct {
	mu      sync.Mutex
	target  uint64 // current target bps (protected by mu)
	rembCap uint64 // hard cap from REMB, 0 = uncapped

	// Delay-based state.
	baselineEMA    float64 // µs, slow EMA — approximates min delay floor
	baselineSeeded bool
	overuseCount   int // consecutive overuse observations

	lastIncrease time.Time
}

// NewGCC returns a GCC controller starting at 300 kbps.
func NewGCC() *GCC {
	return &GCC{
		target:       gccInitialBps,
		lastIncrease: time.Now(),
	}
}

// OnREMB caps the target to the server's estimated maximum.
func (g *GCC) OnREMB(bps uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rembCap = bps
	if g.target > bps {
		g.target = bps
	}
}

// OnTWCC processes a TWCC feedback packet.
func (g *GCC) OnTWCC(pkt *pionrtcp.TransportLayerCC) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// ── Loss-based guard ──────────────────────────────────────────────────
	received, lost := twccLossCounts(pkt)
	total := received + lost
	if total > 0 && float64(lost)/float64(total) > gccLossThreshold {
		g.decrease()
		return
	}

	// ── Delay-based detection ─────────────────────────────────────────────
	// Collect non-negative receive deltas (negative = reordered packet).
	var sum float64
	n := 0
	for _, d := range pkt.RecvDeltas {
		// Delta is int64 microseconds (see pion/rtcp RecvDelta).
		if d.Delta >= 0 {
			sum += float64(d.Delta)
			n++
		}
	}
	if n == 0 {
		g.tryIncrease()
		return
	}
	meanDelta := sum / float64(n)

	// Seed the baseline EMA on first observation.
	if !g.baselineSeeded {
		g.baselineEMA = meanDelta
		g.baselineSeeded = true
		g.tryIncrease()
		return
	}

	// Overuse: current mean delta significantly exceeds the smooth baseline.
	if meanDelta > g.baselineEMA*gccOveruseThreshold {
		g.overuseCount++
	} else {
		if g.overuseCount > 0 {
			g.overuseCount--
		}
	}

	if g.overuseCount >= gccOveruseConsecutive {
		g.decrease()
		g.overuseCount = 0
		// Update baseline toward the new (higher) observed floor.
		g.baselineEMA = gccEMAAlpha*g.baselineEMA + (1-gccEMAAlpha)*meanDelta
		return
	}

	// Normal / underuse: update baseline and probe upward.
	g.baselineEMA = gccEMAAlpha*g.baselineEMA + (1-gccEMAAlpha)*meanDelta
	g.tryIncrease()
}

// TargetBitrate returns the current send target in bps.
func (g *GCC) TargetBitrate() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.target
}

// decrease applies a multiplicative rate reduction and applies the REMB cap.
func (g *GCC) decrease() {
	g.target = uint64(float64(g.target) * gccDecreaseFactor)
	if g.target < gccMinBps {
		g.target = gccMinBps
	}
	g.applyRembCap()
}

// tryIncrease raises the target by one additive step if enough time has passed.
func (g *GCC) tryIncrease() {
	if time.Since(g.lastIncrease) < gccIncreaseDelay {
		return
	}
	g.target += gccIncreaseStep
	if g.target > gccMaxBps {
		g.target = gccMaxBps
	}
	g.applyRembCap()
	g.lastIncrease = time.Now()
}

func (g *GCC) applyRembCap() {
	if g.rembCap > 0 && g.target > g.rembCap {
		g.target = g.rembCap
	}
}

// twccLossCounts returns the number of received and lost packets described
// by the TWCC packet's chunk list.
func twccLossCounts(pkt *pionrtcp.TransportLayerCC) (received, lost int) {
	for _, chunk := range pkt.PacketChunks {
		switch c := chunk.(type) {
		case *pionrtcp.RunLengthChunk:
			if c.PacketStatusSymbol == pionrtcp.TypeTCCPacketNotReceived {
				lost += int(c.RunLength)
			} else {
				received += int(c.RunLength)
			}
		case *pionrtcp.StatusVectorChunk:
			for _, sym := range c.SymbolList {
				if sym == pionrtcp.TypeTCCPacketNotReceived {
					lost++
				} else {
					received++
				}
			}
		}
	}
	return
}
