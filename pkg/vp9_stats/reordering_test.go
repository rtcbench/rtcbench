package vp9_stats

import (
	"testing"

	"github.com/rtcbench/rtcbench/pkg/vp9"
)

func TestFrameCompletenessAfterReordering(t *testing.T) {
	for _, tc := range []struct {
		name string
		seq  []uint16
		lost int64
	}{
		{"in order", []uint16{10, 11, 12, 13}, 0},
		{"gap recovered", []uint16{10, 11, 13, 12}, 0},
		{"first packet late", []uint16{11, 12, 10, 13}, 0},
		{"duplicates", []uint16{10, 11, 11, 12, 13}, 0},
		{"duplicate does not fill gap", []uint16{10, 11, 13, 13}, 1},
		{"gap remains", []uint16{10, 13, 12}, 1},
		{"wrapped gap recovered", []uint16{65534, 0, 65535, 1}, 0},
		{"wrapped first late", []uint16{0, 65535, 1, 65534}, 0},
		{"wrapped gap remains", []uint16{65534, 0, 1}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := NewFrameStatistics(64)
			var callbacks int64
			stats.SetOnFrameLost(func(int64) { callbacks++ })
			pd := vp9.PayloadDescriptor{}
			for i, seq := range tc.seq {
				stats.AcceptPacket(int64(100000+i), seq, 9000, 1000, &pd)
			}
			stats.AcceptPacket(133333, 100, 12000, 1000, &pd)
			if stats.framesLost != tc.lost || stats.framesComplete != 1-tc.lost || callbacks != tc.lost {
				t.Fatalf("complete=%d lost=%d callbacks=%d; want lost=%d", stats.framesComplete, stats.framesLost, callbacks, tc.lost)
			}
		})
	}
}
