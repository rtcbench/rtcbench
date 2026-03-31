package metricsserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

func TestNew(t *testing.T) {
	s := New(8080)
	if s == nil {
		t.Fatalf("New returned nil")
	}
	if s.viewers == nil {
		t.Fatalf("viewers map is nil")
	}
	if len(s.viewers) != 0 {
		t.Fatalf("viewers map should be empty, got %d entries", len(s.viewers))
	}
	if s.port != 8080 {
		t.Fatalf("port = %d, want 8080", s.port)
	}
}

func TestServer_Subscriber_UpdatesState(t *testing.T) {
	s := New(0)
	sub := s.Subscriber()

	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "v1",
		SmoothBitrate: 3_500_000,
		SmoothFPS:     25,
	})

	s.mu.RLock()
	vs, ok := s.viewers["v1"]
	s.mu.RUnlock()
	if !ok {
		t.Fatalf("viewer v1 not found in map")
	}
	if vs.SmoothBitrateBps != 3_500_000 {
		t.Fatalf("SmoothBitrateBps = %f, want 3500000", vs.SmoothBitrateBps)
	}
	if vs.SmoothFPS != 25 {
		t.Fatalf("SmoothFPS = %f, want 25", vs.SmoothFPS)
	}
	if vs.SampleCount != 1 {
		t.Fatalf("SampleCount = %d, want 1", vs.SampleCount)
	}

	// Send second sample with updated bitrate.
	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "v1",
		SmoothBitrate: 4_000_000,
		SmoothFPS:     30,
	})

	s.mu.RLock()
	vs = s.viewers["v1"]
	s.mu.RUnlock()
	if vs.SampleCount != 2 {
		t.Fatalf("SampleCount = %d, want 2", vs.SampleCount)
	}
	if vs.SmoothBitrateBps != 4_000_000 {
		t.Fatalf("SmoothBitrateBps = %f, want 4000000", vs.SmoothBitrateBps)
	}
	if vs.SmoothFPS != 30 {
		t.Fatalf("SmoothFPS = %f, want 30", vs.SmoothFPS)
	}
}

func TestServer_Subscriber_MultipleViewers(t *testing.T) {
	s := New(0)
	sub := s.Subscriber()

	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "v1",
		SmoothBitrate: 1_000_000,
		SmoothFPS:     25,
	})
	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "v2",
		SmoothBitrate: 2_000_000,
		SmoothFPS:     30,
	})

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.viewers) != 2 {
		t.Fatalf("len(viewers) = %d, want 2", len(s.viewers))
	}
	if _, ok := s.viewers["v1"]; !ok {
		t.Fatalf("viewer v1 not found")
	}
	if _, ok := s.viewers["v2"]; !ok {
		t.Fatalf("viewer v2 not found")
	}
}

func TestServer_handleHealth_NoViewers(t *testing.T) {
	s := New(0)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)

	s.handleHealth(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.Status != "no_data" {
		t.Fatalf("Status = %q, want %q", resp.Status, "no_data")
	}
	if resp.ViewersTotal != 0 {
		t.Fatalf("ViewersTotal = %d, want 0", resp.ViewersTotal)
	}
	if resp.ViewersActive != 0 {
		t.Fatalf("ViewersActive = %d, want 0", resp.ViewersActive)
	}
	if resp.AggregateBitrateMbps != 0 {
		t.Fatalf("AggregateBitrateMbps = %f, want 0", resp.AggregateBitrateMbps)
	}
	if len(resp.Viewers) != 0 {
		t.Fatalf("len(Viewers) = %d, want 0", len(resp.Viewers))
	}
}

func TestServer_handleHealth_ActiveViewers(t *testing.T) {
	s := New(0)
	sub := s.Subscriber()

	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "v1",
		SmoothBitrate: 3_000_000,
		SmoothFPS:     25,
	})
	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "v2",
		SmoothBitrate: 2_000_000,
		SmoothFPS:     20,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	s.handleHealth(rec, req)

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("Status = %q, want %q", resp.Status, "ok")
	}
	if resp.ViewersTotal != 2 {
		t.Fatalf("ViewersTotal = %d, want 2", resp.ViewersTotal)
	}
	if resp.ViewersActive != 2 {
		t.Fatalf("ViewersActive = %d, want 2", resp.ViewersActive)
	}
	// Expected: (3_000_000 + 2_000_000) / 1_000_000 = 5.0
	wantMbps := float32(5.0)
	if resp.AggregateBitrateMbps != wantMbps {
		t.Fatalf("AggregateBitrateMbps = %f, want %f", resp.AggregateBitrateMbps, wantMbps)
	}
	if len(resp.Viewers) != 2 {
		t.Fatalf("len(Viewers) = %d, want 2", len(resp.Viewers))
	}
}

func TestServer_handleHealth_StaleViewers(t *testing.T) {
	s := New(0)
	sub := s.Subscriber()

	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "stale",
		SmoothBitrate: 1_000_000,
		SmoothFPS:     25,
	})

	// Manually set LastSeenAt to 60 seconds ago.
	s.mu.Lock()
	s.viewers["stale"].LastSeenAt = time.Now().Add(-60 * time.Second)
	s.mu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	s.handleHealth(rec, req)

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.ViewersTotal != 1 {
		t.Fatalf("ViewersTotal = %d, want 1", resp.ViewersTotal)
	}
	if resp.ViewersActive != 0 {
		t.Fatalf("ViewersActive = %d, want 0", resp.ViewersActive)
	}
	if resp.Status != "no_data" {
		t.Fatalf("Status = %q, want %q", resp.Status, "no_data")
	}
	if len(resp.Viewers) != 1 {
		t.Fatalf("len(Viewers) = %d, want 1", len(resp.Viewers))
	}
	if resp.Viewers[0].LastSeenAgoMs < 60000 {
		t.Fatalf("LastSeenAgoMs = %d, want >= 60000", resp.Viewers[0].LastSeenAgoMs)
	}
}

func TestServer_handleHealth_MixedActiveStale(t *testing.T) {
	s := New(0)
	sub := s.Subscriber()

	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "active",
		SmoothBitrate: 3_000_000,
		SmoothFPS:     25,
	})
	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "stale",
		SmoothBitrate: 2_000_000,
		SmoothFPS:     20,
	})

	// Make one viewer stale.
	s.mu.Lock()
	s.viewers["stale"].LastSeenAt = time.Now().Add(-60 * time.Second)
	s.mu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	s.handleHealth(rec, req)

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.ViewersTotal != 2 {
		t.Fatalf("ViewersTotal = %d, want 2", resp.ViewersTotal)
	}
	if resp.ViewersActive != 1 {
		t.Fatalf("ViewersActive = %d, want 1", resp.ViewersActive)
	}
	if resp.Status != "ok" {
		t.Fatalf("Status = %q, want %q", resp.Status, "ok")
	}
	// Only the active viewer's bitrate should be aggregated: 3_000_000 / 1_000_000 = 3.0
	wantMbps := float32(3.0)
	if resp.AggregateBitrateMbps != wantMbps {
		t.Fatalf("AggregateBitrateMbps = %f, want %f", resp.AggregateBitrateMbps, wantMbps)
	}
}

func TestServer_handleHealth_Errors(t *testing.T) {
	s := New(0)
	sub := s.Subscriber()

	period := vp9_stats.Period{}
	period.Errors[vp9_stats.ErrViewerSeqNoJumpID] = 7
	period.Errors[vp9_stats.ErrViewerPcapWriteID] = 3

	sub(period, vp9_stats.VideoQualitySample{
		Nickname:      "v1",
		SmoothBitrate: 1_000_000,
		SmoothFPS:     25,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	s.handleHealth(rec, req)

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if len(resp.Errors) != 2 {
		t.Fatalf("len(Errors) = %d, want 2", len(resp.Errors))
	}
	if resp.Errors["vp9_stats.Publisher.ErrViewerSeqNoJumpID"] != 7 {
		t.Fatalf("ErrViewerSeqNoJumpID = %d, want 7", resp.Errors["vp9_stats.Publisher.ErrViewerSeqNoJumpID"])
	}
	if resp.Errors["vp9_stats.Publisher.ErrViewerPcapWriteID"] != 3 {
		t.Fatalf("ErrViewerPcapWriteID = %d, want 3", resp.Errors["vp9_stats.Publisher.ErrViewerPcapWriteID"])
	}
}

func TestServer_ListenAndServe_ContextCancellation(t *testing.T) {
	s := New(19876)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ListenAndServe(ctx)
	}()

	// Give the server a moment to start listening.
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ListenAndServe returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("ListenAndServe did not return within 5 seconds after context cancellation")
	}
}
