package ivf

import (
	"testing"
)

func TestTemporalLayerID(t *testing.T) {
	tests := []struct {
		name   string
		numTL  int
		expect []uint8
	}{
		{
			name:   "1 temporal layer",
			numTL:  1,
			expect: []uint8{0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			name:   "2 temporal layers",
			numTL:  2,
			expect: []uint8{0, 1, 0, 1, 0, 1, 0, 1},
		},
		{
			name:   "3 temporal layers",
			numTL:  3,
			expect: []uint8{0, 2, 1, 2, 0, 2, 1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, want := range tt.expect {
				got := TemporalLayerID(i, tt.numTL)
				if got != want {
					t.Errorf("frame %d: got TID=%d, want %d", i, got, want)
				}
			}
		})
	}
}

// cfg3x3 is a standard 3-spatial x 3-temporal SVC config at 3.5 Mbps.
// Layer thresholds (with 90% hysteresis):
//
//	(S2,T2) need >= 3,150,000   (S1,T2) need >=   913,500   (S0,T2) need >= 189,000
//	(S2,T1) need >= 2,079,000   (S1,T1) need >=   602,910   (S0,T1) need >= 124,740
//	(S2,T0) need >= 1,039,500   (S1,T0) need >=   301,455   (S0,T0) need >=  62,370
var cfg3x3 = SVCConfig{
	NumSpatialLayers:  3,
	NumTemporalLayers: 3,
	Widths:            []uint16{640, 1280, 1920},
	Heights:           []uint16{360, 720, 1080},
	TargetBitrateBps:  3_500_000,
}

func TestSelectLayers(t *testing.T) {
	tests := []struct {
		name       string
		bitrateBps int
		cfg        SVCConfig
		wantSID    uint8
		wantTID    uint8
	}{
		{
			name:       "zero bitrate returns base layer",
			bitrateBps: 0,
			cfg:        cfg3x3,
			wantSID:    0,
			wantTID:    0,
		},
		{
			name:       "full bandwidth returns all layers",
			bitrateBps: 3_500_000,
			cfg:        cfg3x3,
			wantSID:    2,
			wantTID:    2,
		},
		{
			name:       "above 90% threshold still returns all layers",
			bitrateBps: 3_200_000,
			cfg:        cfg3x3,
			wantSID:    2,
			wantTID:    2,
		},
		{
			name:       "below S2T2 threshold drops to S2T1",
			bitrateBps: 3_000_000,
			cfg:        cfg3x3,
			wantSID:    2,
			wantTID:    1,
		},
		{
			name:       "at S2T1 threshold",
			bitrateBps: 2_100_000,
			cfg:        cfg3x3,
			wantSID:    2,
			wantTID:    1,
		},
		{
			name:       "below S2T1 drops to S2T0",
			bitrateBps: 2_000_000,
			cfg:        cfg3x3,
			wantSID:    2,
			wantTID:    0,
		},
		{
			name:       "below S2T0 drops to S1T2",
			bitrateBps: 1_000_000,
			cfg:        cfg3x3,
			wantSID:    1,
			wantTID:    2,
		},
		{
			name:       "below S1T2 drops to S1T1",
			bitrateBps: 900_000,
			cfg:        cfg3x3,
			wantSID:    1,
			wantTID:    1,
		},
		{
			name:       "below S1T1 drops to S1T0",
			bitrateBps: 500_000,
			cfg:        cfg3x3,
			wantSID:    1,
			wantTID:    0,
		},
		{
			name:       "below S1T0 drops to S0T2",
			bitrateBps: 250_000,
			cfg:        cfg3x3,
			wantSID:    0,
			wantTID:    2,
		},
		{
			name:       "below S0T2 drops to S0T1",
			bitrateBps: 150_000,
			cfg:        cfg3x3,
			wantSID:    0,
			wantTID:    1,
		},
		{
			name:       "below S0T1 drops to S0T0",
			bitrateBps: 100_000,
			cfg:        cfg3x3,
			wantSID:    0,
			wantTID:    0,
		},
		{
			name:       "very low bitrate still gets base layer",
			bitrateBps: 63_000,
			cfg:        cfg3x3,
			wantSID:    0,
			wantTID:    0,
		},
		{
			name:       "below S0T0 threshold returns base layer",
			bitrateBps: 50_000,
			cfg:        cfg3x3,
			wantSID:    0,
			wantTID:    0,
		},
		{
			name: "single layer config always returns 0,0",
			bitrateBps: 1_000_000,
			cfg: SVCConfig{
				NumSpatialLayers:  1,
				NumTemporalLayers: 1,
				Widths:            []uint16{1920},
				Heights:           []uint16{1080},
				TargetBitrateBps:  3_500_000,
			},
			wantSID: 0,
			wantTID: 0,
		},
		{
			name:       "no target bitrate sends all layers",
			bitrateBps: 100_000,
			cfg: SVCConfig{
				NumSpatialLayers:  3,
				NumTemporalLayers: 3,
				TargetBitrateBps:  0,
			},
			wantSID: 2,
			wantTID: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sid, tid := selectLayers(tt.bitrateBps, tt.cfg)
			if sid != tt.wantSID || tid != tt.wantTID {
				t.Errorf("selectLayers(%d) = SID=%d,TID=%d; want SID=%d,TID=%d",
					tt.bitrateBps, sid, tid, tt.wantSID, tt.wantTID)
			}
		})
	}
}

func TestSelectLayersDropsTemporalFirst(t *testing.T) {
	// Walk bitrate from high to low and verify that TID drops before SID.
	prevSID, prevTID := selectLayers(5_000_000, cfg3x3)
	for bps := 4_900_000; bps > 50_000; bps -= 10_000 {
		sid, tid := selectLayers(bps, cfg3x3)
		if sid > prevSID {
			t.Errorf("at %d bps: SID increased from %d to %d", bps, prevSID, sid)
		}
		if sid == prevSID && tid > prevTID {
			t.Errorf("at %d bps: TID increased from %d to %d at same SID=%d", bps, prevTID, tid, sid)
		}
		prevSID, prevTID = sid, tid
	}

	// Verify we reached the base layer at the bottom
	sid, tid := selectLayers(50_000, cfg3x3)
	if sid != 0 || tid != 0 {
		t.Errorf("at 50kbps: expected SID=0,TID=0, got SID=%d,TID=%d", sid, tid)
	}
}

func TestLayerBitrateThreshold(t *testing.T) {
	// Verify specific threshold values for the 3x3 config.
	// Full layer set (S2,T2) should equal the target bitrate.
	full := layerBitrateThreshold(2, 2, cfg3x3)
	if full != 3_500_000 {
		t.Errorf("(S2,T2) threshold: got %d, want 3500000", full)
	}

	// Base layer (S0,T0) should be ~2% of total (0.06 * 0.33 = 0.0198).
	base := layerBitrateThreshold(0, 0, cfg3x3)
	if base != 69_300 {
		t.Errorf("(S0,T0) threshold: got %d, want 69300", base)
	}

	// Zero target returns zero threshold.
	zeroCfg := SVCConfig{NumSpatialLayers: 3, NumTemporalLayers: 3}
	if v := layerBitrateThreshold(2, 2, zeroCfg); v != 0 {
		t.Errorf("zero target: got %d, want 0", v)
	}
}
