package pionutil

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestStatsReportRTTUsesSelectedCandidatePair(t *testing.T) {
	report := webrtc.StatsReport{
		"transport-1": webrtc.TransportStats{
			ID:                      "transport-1",
			Type:                    webrtc.StatsTypeTransport,
			SelectedCandidatePairID: "pair-1",
		},
		"pair-1": webrtc.ICECandidatePairStats{
			ID:                   "pair-1",
			Type:                 webrtc.StatsTypeCandidatePair,
			State:                webrtc.StatsICECandidatePairStateSucceeded,
			CurrentRoundTripTime: 0.125,
		},
	}

	rtt, ok := statsReportRTT(report)
	if !ok {
		t.Fatal("statsReportRTT() ok = false, want true")
	}
	if rtt != 125*time.Millisecond {
		t.Fatalf("statsReportRTT() = %v, want 125ms", rtt)
	}
}

func TestStatsReportRTTFallsBackToRemoteStats(t *testing.T) {
	report := webrtc.StatsReport{
		"remote-outbound-1": webrtc.RemoteOutboundRTPStreamStats{
			ID:            "remote-outbound-1",
			Type:          webrtc.StatsTypeRemoteOutboundRTP,
			RoundTripTime: 0.08,
		},
	}

	rtt, ok := statsReportRTT(report)
	if !ok {
		t.Fatal("statsReportRTT() ok = false, want true")
	}
	if rtt != 80*time.Millisecond {
		t.Fatalf("statsReportRTT() = %v, want 80ms", rtt)
	}
}
