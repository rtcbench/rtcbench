package core

import (
	"testing"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

func TestImpairmentConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *YAMLImpairmentConfig
		wantErr bool
	}{
		{
			name: "valid minimal",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"default": {},
				},
				Default: "default",
			},
			wantErr: false,
		},
		{
			name: "valid with supported fields",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {
						BandwidthKbps: intPtr(1000),
						Latency:       strPtr("50ms"),
						LossPercent:   intPtr(5),
						Seed:          int64Ptr(42),
					},
				},
				Default: "test",
			},
			wantErr: false,
		},
		{
			name: "jitter rejected (not implemented)",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {
						Jitter: strPtr("10ms"),
					},
				},
				Default: "test",
			},
			wantErr: true,
		},
		{
			name: "empty profiles",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{},
				Default:  "default",
			},
			wantErr: true,
		},
		{
			name: "missing default",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"default": {},
				},
				Default: "",
			},
			wantErr: true,
		},
		{
			name: "unknown default",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {},
				},
				Default: "unknown",
			},
			wantErr: true,
		},
		{
			name: "negative bandwidth",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {
						BandwidthKbps: intPtr(-100),
					},
				},
				Default: "test",
			},
			wantErr: true,
		},
		{
			name: "loss out of range",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {
						LossPercent: intPtr(150),
					},
				},
				Default: "test",
			},
			wantErr: true,
		},
		{
			name: "valid assignment",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"low":  {},
					"high": {},
				},
				Assignments: []YAMLImpairmentAssignment{
					{
						Match: YAMLAssignmentSelector{
							Role: strPtr("viewer"),
						},
						Profile: "low",
					},
				},
				Default: "high",
			},
			wantErr: false,
		},
		{
			name: "assignment unknown profile",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {},
				},
				Assignments: []YAMLImpairmentAssignment{
					{
						Match: YAMLAssignmentSelector{
							Role: strPtr("viewer"),
						},
						Profile: "unknown",
					},
				},
				Default: "test",
			},
			wantErr: true,
		},
		{
			name: "assignment empty profile",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {},
				},
				Assignments: []YAMLImpairmentAssignment{
					{
						Match:   YAMLAssignmentSelector{},
						Profile: "",
					},
				},
				Default: "test",
			},
			wantErr: true,
		},
		{
			name: "invalid role",
			cfg: &YAMLImpairmentConfig{
				Profiles: map[string]*YAMLImpairmentProfile{
					"test": {},
				},
				Assignments: []YAMLImpairmentAssignment{
					{
						Match: YAMLAssignmentSelector{
							Role: strPtr("invalid"),
						},
						Profile: "test",
					},
				},
				Default: "test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateImpairment(tt.cfg)
			if (len(errs) > 0) != tt.wantErr {
				t.Errorf("validateImpairment() error = %v, wantErr %v", errs, tt.wantErr)
			}
		})
	}
}

func TestNetworkConfigAllowsImpairmentWithoutServerPort(t *testing.T) {
	serverIP := "127.0.0.1"
	cfg := &YAMLNetworkConfig{
		ServerIP: &serverIP,
		Impairment: &YAMLImpairmentConfig{
			Profiles: map[string]*YAMLImpairmentProfile{
				"default": {},
			},
			Default: "default",
		},
	}

	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestResolveImpairmentProfile(t *testing.T) {
	defaultProfile := &ImpairmentProfile{Name: "default"}
	viewerProfile := &ImpairmentProfile{Name: "viewer-specific"}

	cfg := &ImpairmentConfig{
		Profiles: map[string]*ImpairmentProfile{
			"default":         defaultProfile,
			"viewer-specific": viewerProfile,
		},
		Assignments: []ImpairmentAssignment{
			{
				Match:   AssignmentSelector{Role: "viewer"},
				Profile: "viewer-specific",
			},
		},
		Default: "default",
	}

	tests := []struct {
		name   string
		role   UserRole
		index  int64
		expect *ImpairmentProfile
	}{
		{
			name:   "sender gets default",
			role:   Sender,
			index:  0,
			expect: defaultProfile,
		},
		{
			name:   "viewer gets viewer-specific",
			role:   Viewer,
			index:  0,
			expect: viewerProfile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &UserConfig{Role: tt.role}
			got := resolveImpairmentProfile(cfg, uc, tt.index)
			if got != tt.expect {
				t.Errorf("resolveImpairmentProfile() = %v, want %v", got.Name, tt.expect.Name)
			}
		})
	}
}

func TestReceiverThresholdEvaluation(t *testing.T) {
	summary := ReceiverSummary{
		Scenario:            "test",
		Plugin:              "janus",
		Profile:             "bad-network",
		UserID:              "user1",
		FreezeCount:         10,
		FreezeDurationTotal: 5.0,
		FrameLossRatio:      0.15,
		MeanBitrateBps:      500000,
		MeanFPS:             15.0,
		MaxJitterUS:         50000,
		MeanRTTMS:           85.0,
		MaxRTTMS:            120.0,
		PliCount:            100,
	}

	tests := []struct {
		name      string
		threshold ReceiverThresholdConfig
		wantErr   bool
	}{
		{
			name: "pass all",
			threshold: ReceiverThresholdConfig{
				MaxFreezeCount:    int64Ptr(20),
				MaxFreezeDuration: float64Ptr(10.0),
				MaxFrameLossRatio: float64Ptr(0.5),
				MinBitrateBps:     float64Ptr(100000),
				MinFPS:            float64Ptr(5.0),
				MaxJitterUS:       float64Ptr(100000),
				MaxPLICount:       int64Ptr(200),
			},
			wantErr: false,
		},
		{
			name: "fail freeze count",
			threshold: ReceiverThresholdConfig{
				MaxFreezeCount: int64Ptr(5),
			},
			wantErr: true,
		},
		{
			name: "fail freeze duration",
			threshold: ReceiverThresholdConfig{
				MaxFreezeDuration: float64Ptr(2.0),
			},
			wantErr: true,
		},
		{
			name: "fail frame loss",
			threshold: ReceiverThresholdConfig{
				MaxFrameLossRatio: float64Ptr(0.05),
			},
			wantErr: true,
		},
		{
			name: "fail min bitrate",
			threshold: ReceiverThresholdConfig{
				MinBitrateBps: float64Ptr(1000000),
			},
			wantErr: true,
		},
		{
			name: "fail min fps",
			threshold: ReceiverThresholdConfig{
				MinFPS: float64Ptr(25.0),
			},
			wantErr: true,
		},
		{
			name: "fail max jitter",
			threshold: ReceiverThresholdConfig{
				MaxJitterUS: float64Ptr(10000),
			},
			wantErr: true,
		},
		{
			name: "fail mean rtt",
			threshold: ReceiverThresholdConfig{
				MaxMeanRTTMS: float64Ptr(50.0),
			},
			wantErr: true,
		},
		{
			name: "fail pli count",
			threshold: ReceiverThresholdConfig{
				MaxPLICount: int64Ptr(50),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evaluateReceiverThreshold(tt.threshold, summary)
			if (err != nil) != tt.wantErr {
				t.Errorf("evaluateReceiverThreshold() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFreezeDetector(t *testing.T) {
	detector := NewFreezeDetector(5.0, 200)

	t0 := time.Now()
	_, _, _ = detector.Update(t0, 10.0)
	_, _, _ = detector.Update(t0.Add(100*time.Millisecond), 10.0)
	_, _, _ = detector.Update(t0.Add(200*time.Millisecond), 10.0)

	_, _, _ = detector.Update(t0.Add(300*time.Millisecond), 2.0)
	_, _, _ = detector.Update(t0.Add(400*time.Millisecond), 1.0)

	_, ended, dur := detector.Update(t0.Add(600*time.Millisecond), 10.0)

	count, total := detector.Stats()

	if !ended {
		t.Error("expected freeze to have ended")
	}
	if count != 1 {
		t.Errorf("freeze count = %d, want 1", count)
	}
	if dur < 200*time.Millisecond || dur > 400*time.Millisecond {
		t.Errorf("freeze duration = %v, want ~300ms", dur)
	}
	if total < 200*time.Millisecond {
		t.Errorf("total duration = %v, want >200ms", total)
	}
}

func TestReceiverMetricsPipeline(t *testing.T) {
	m := newRunMetricsCollector()
	m.RegisterReceiver("viewer-1", "janus", "default", "lossy-wifi")

	for i := 0; i < 10; i++ {
		m.ObserveReceiverSample("viewer-1[101]", vp9_stats.VideoQualitySample{
			Nickname:         "viewer-1[101]",
			SmoothBitrate:    1_500_000,
			DecoderSmoothFPS: 24,
			FrameJitterUS:    1200,
			FramesComplete:   int64(100 * (i + 1)),
			FramesLost:       int64(5 * (i + 1)),
			RTCP:             vp9_stats.RTCPData{PLISent: int64(i)},
		})
	}
	m.ObserveReceiverRTT("viewer-1", 40*time.Millisecond)
	m.ObserveReceiverRTT("viewer-1", 80*time.Millisecond)

	snap := m.Snapshot()
	if len(snap.Receivers) != 1 {
		t.Fatalf("Snapshot.Receivers length = %d, want 1", len(snap.Receivers))
	}
	r := snap.Receivers[0]
	if r.UserID != "viewer-1[101]" || r.Profile != "lossy-wifi" || r.Plugin != "janus" {
		t.Errorf("unexpected labels: %+v", r)
	}
	if r.MeanBitrateBps < 1.4e6 || r.MeanBitrateBps > 1.6e6 {
		t.Errorf("MeanBitrateBps = %v, want ~1.5M", r.MeanBitrateBps)
	}
	if r.MeanFPS < 23 || r.MeanFPS > 25 {
		t.Errorf("MeanFPS = %v, want ~24", r.MeanFPS)
	}
	if r.PliCount != 9 {
		t.Errorf("PliCount = %d, want 9", r.PliCount)
	}
	if r.MeanRTTMS != 60 {
		t.Errorf("MeanRTTMS = %v, want 60", r.MeanRTTMS)
	}
	if r.MaxRTTMS != 80 {
		t.Errorf("MaxRTTMS = %v, want 80", r.MaxRTTMS)
	}
}

func TestReceiverMetricsUnregisterArchives(t *testing.T) {
	m := newRunMetricsCollector()
	m.RegisterReceiver("viewer-ephemeral", "janus", "churn", "5g")
	m.ObserveReceiverSample("viewer-ephemeral[202]", vp9_stats.VideoQualitySample{
		Nickname:      "viewer-ephemeral[202]",
		SmoothBitrate: 800_000,
	})

	archived := m.UnregisterReceiver("viewer-ephemeral")
	if archived.UserID != "viewer-ephemeral[202]" {
		t.Fatalf("UnregisterReceiver returned %+v", archived)
	}

	m.ObserveReceiverSample("viewer-ephemeral", vp9_stats.VideoQualitySample{})

	snap := m.Snapshot()
	if len(snap.Receivers) != 1 {
		t.Fatalf("archived summary not visible in snapshot: %+v", snap.Receivers)
	}
	if snap.Receivers[0].Profile != "5g" {
		t.Errorf("archived summary profile = %q, want 5g", snap.Receivers[0].Profile)
	}
}

func TestAssignmentMatches(t *testing.T) {
	tests := []struct {
		name     string
		sel      AssignmentSelector
		role     UserRole
		userID   string
		index    int64
		expected bool
	}{
		{
			name:     "empty selector matches any index",
			sel:      AssignmentSelector{},
			role:     Sender,
			userID:   "user1",
			index:    42,
			expected: true,
		},
		{
			name:     "role mismatch",
			sel:      AssignmentSelector{Role: "viewer"},
			role:     Sender,
			userID:   "user1",
			index:    0,
			expected: false,
		},
		{
			name:     "role match ignores index when no range set",
			sel:      AssignmentSelector{Role: "viewer"},
			role:     Viewer,
			userID:   "user1",
			index:    9999,
			expected: true,
		},
		{
			name:     "index below min",
			sel:      AssignmentSelector{UserIndexMin: intPtr(5)},
			role:     Viewer,
			userID:   "user1",
			index:    3,
			expected: false,
		},
		{
			name:     "index above max",
			sel:      AssignmentSelector{UserIndexMax: intPtr(5)},
			role:     Viewer,
			userID:   "user1",
			index:    10,
			expected: false,
		},
		{
			name:     "index in range",
			sel:      AssignmentSelector{UserIndexMin: intPtr(5), UserIndexMax: intPtr(10)},
			role:     Viewer,
			userID:   "user1",
			index:    7,
			expected: true,
		},
		{
			name:     "exact index 0 with min=0 max=0",
			sel:      AssignmentSelector{UserIndexMin: intPtr(0), UserIndexMax: intPtr(0)},
			role:     Viewer,
			userID:   "user1",
			index:    0,
			expected: true,
		},
		{
			name:     "exact index 0 rejects index 1",
			sel:      AssignmentSelector{UserIndexMin: intPtr(0), UserIndexMax: intPtr(0)},
			role:     Viewer,
			userID:   "user1",
			index:    1,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &UserConfig{Role: tt.role, UserID: tt.userID}
			got := assignmentMatches(tt.sel, uc, tt.index)
			if got != tt.expected {
				t.Errorf("assignmentMatches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func intPtr(v int) *int {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func strPtr(v string) *string {
	return &v
}
