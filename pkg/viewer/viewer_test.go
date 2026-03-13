package viewer

import (
	"errors"
	"strings"
	"testing"

	"call.zip/pkg/log"
	"call.zip/pkg/vp9_stats"
)

func validConfig() Config {
	return Config{
		VP9RTPPayloadType: 98,
		TrackBufferSize:   1500,
		StatsBufferSize:   768,
	}
}

func TestConfig_verify_Valid(t *testing.T) {
	c := validConfig()
	if err := c.verify(); err != nil {
		t.Fatalf("expected nil error for valid config, got: %v", err)
	}
}

func TestConfig_verify_PayloadType(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"below range", -1, true},
		{"min boundary", 0, false},
		{"max boundary", 255, false},
		{"above range", 256, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.VP9RTPPayloadType = tt.value
			err := c.verify()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for VP9RTPPayloadType=%d, got nil", tt.value)
				}
				if !strings.Contains(err.Error(), "VP9RTPPayloadType") {
					t.Fatalf("error should mention VP9RTPPayloadType, got: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected nil error for VP9RTPPayloadType=%d, got: %v", tt.value, err)
				}
			}
		})
	}
}

func TestConfig_verify_TrackBufferSize(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"below range", 99, true},
		{"min boundary", 100, false},
		{"max boundary", 10000, false},
		{"above range", 10001, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.TrackBufferSize = tt.value
			err := c.verify()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for TrackBufferSize=%d, got nil", tt.value)
				}
				if !strings.Contains(err.Error(), "TrackBufferSize") {
					t.Fatalf("error should mention TrackBufferSize, got: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected nil error for TrackBufferSize=%d, got: %v", tt.value, err)
				}
			}
		})
	}
}

func TestConfig_verify_StatsBufferSize(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"below range", 63, true},
		{"min boundary", 64, false},
		{"max boundary", 4096, false},
		{"above range", 4097, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.StatsBufferSize = tt.value
			err := c.verify()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for StatsBufferSize=%d, got nil", tt.value)
				}
				if !strings.Contains(err.Error(), "StatsBufferSize") {
					t.Fatalf("error should mention StatsBufferSize, got: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected nil error for StatsBufferSize=%d, got: %v", tt.value, err)
				}
			}
		})
	}
}

func TestJoinErrors(t *testing.T) {
	baseErr := errors.New("base")
	var errs [numViewerErrIDs]int64
	errs[errViewerReadFromTrackID] = 3
	errs[errViewerSeqNoJumpID] = 7

	result := joinErrors(baseErr, errs)
	if result == nil {
		t.Fatalf("expected non-nil error from joinErrors")
	}
	if !errors.Is(result, baseErr) {
		t.Fatalf("expected result to wrap baseErr via errors.Is")
	}
	if !strings.Contains(result.Error(), "total viewer errs") {
		t.Fatalf("expected error message to contain 'total viewer errs', got: %v", result)
	}
}

func TestNewManager(t *testing.T) {
	ch := make(chan vp9_stats.VideoQualitySample, 1)
	m := NewManager(ch, log.NewRegistry(nil, nil).NewLogger("test", ""))
	if m == nil {
		t.Fatalf("expected non-nil Manager")
	}
	if got := m.Size(); got != 0 {
		t.Fatalf("expected Size() == 0, got %d", got)
	}
}

func TestNewManager_SizeZero(t *testing.T) {
	ch := make(chan vp9_stats.VideoQualitySample, 1)
	m := NewManager(ch, log.NewRegistry(nil, nil).NewLogger("test", ""))
	for i := 0; i < 3; i++ {
		if got := m.Size(); got != 0 {
			t.Fatalf("call %d: expected Size() == 0, got %d", i, got)
		}
	}
}
