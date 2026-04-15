package pionutil

import (
	"context"
	"time"

	"github.com/pion/webrtc/v4"
)

func CurrentPeerConnectionRTT(pc *webrtc.PeerConnection) (time.Duration, bool) {
	if pc == nil {
		return 0, false
	}
	return statsReportRTT(pc.GetStats())
}

func PollPeerConnectionRTT(
	ctx context.Context,
	interval time.Duration,
	pcSource func() *webrtc.PeerConnection,
	observe func(time.Duration),
) {
	if ctx == nil || pcSource == nil || observe == nil {
		return
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}

	sample := func() {
		rtt, ok := CurrentPeerConnectionRTT(pcSource())
		if ok {
			observe(rtt)
		}
	}

	sample()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample()
		}
	}
}

func statsReportRTT(report webrtc.StatsReport) (time.Duration, bool) {
	for _, stat := range report {
		transport, ok := stat.(webrtc.TransportStats)
		if !ok || transport.SelectedCandidatePairID == "" {
			continue
		}
		pair, ok := report[transport.SelectedCandidatePairID].(webrtc.ICECandidatePairStats)
		if !ok {
			continue
		}
		if rtt := durationFromSeconds(pair.CurrentRoundTripTime); rtt > 0 {
			return rtt, true
		}
	}

	var (
		bestPair    webrtc.ICECandidatePairStats
		bestPackets uint64
		havePair    bool
	)
	for _, stat := range report {
		pair, ok := stat.(webrtc.ICECandidatePairStats)
		if !ok || pair.State != webrtc.StatsICECandidatePairStateSucceeded {
			continue
		}
		rtt := durationFromSeconds(pair.CurrentRoundTripTime)
		if rtt <= 0 {
			continue
		}
		packets := uint64(pair.PacketsSent) + uint64(pair.PacketsReceived)
		if !havePair || (pair.Nominated && !bestPair.Nominated) || (pair.Nominated == bestPair.Nominated && packets > bestPackets) {
			bestPair = pair
			bestPackets = packets
			havePair = true
		}
	}
	if havePair {
		return durationFromSeconds(bestPair.CurrentRoundTripTime), true
	}

	var bestRTT time.Duration
	for _, stat := range report {
		switch remote := stat.(type) {
		case webrtc.RemoteInboundRTPStreamStats:
			if rtt := durationFromSeconds(remote.RoundTripTime); rtt > bestRTT {
				bestRTT = rtt
			}
		case webrtc.RemoteOutboundRTPStreamStats:
			if rtt := durationFromSeconds(remote.RoundTripTime); rtt > bestRTT {
				bestRTT = rtt
			}
		}
	}
	if bestRTT > 0 {
		return bestRTT, true
	}

	return 0, false
}

func durationFromSeconds(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}
