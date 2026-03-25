package statsjsonl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"call.zip/pkg/vp9_stats"
)

func TestNewCreatesFile(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "testhost", 0)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w.Close()

	path := filepath.Join(dir, "bench-callzip-testhost-0.jsonl")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected file %s to exist", path)
	}
}

func TestNewInvalidDir(t *testing.T) {
	_, err := New("/nonexistent-dir-xyz", "host", 0)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestSubscriberWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "node-01", 0)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sub := w.Subscriber()
	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "viewer-1",
		SmoothBitrate: 3500000,
		SmoothFPS:     25.0,
	})
	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "viewer-2",
		SmoothBitrate: 3400000,
		SmoothFPS:     24.5,
	})

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "bench-callzip-node-01-0.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var s1, s2 Sample
	if err := json.Unmarshal([]byte(lines[0]), &s1); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &s2); err != nil {
		t.Fatalf("unmarshal line 2: %v", err)
	}

	if s1.Client != "callzip" {
		t.Errorf("expected client=callzip, got %q", s1.Client)
	}
	if s1.Host != "node-01" {
		t.Errorf("expected host=node-01, got %q", s1.Host)
	}
	if s1.Viewer != "viewer-1" {
		t.Errorf("expected viewer=viewer-1, got %q", s1.Viewer)
	}
	if s1.BitrateBps != 3500000 {
		t.Errorf("expected bitrate_bps=3500000, got %f", s1.BitrateBps)
	}
	if s1.FPS != 25.0 {
		t.Errorf("expected fps=25, got %f", s1.FPS)
	}
	if s1.Timestamp <= 0 {
		t.Error("expected positive timestamp")
	}

	if s2.Viewer != "viewer-2" {
		t.Errorf("expected viewer=viewer-2, got %q", s2.Viewer)
	}
}

func TestSubscriberWritesErrors(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "node-01", 0)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	period := vp9_stats.Period{}
	period.Errors[vp9_stats.ErrViewerSeqNoJumpID] = 5
	period.Errors[vp9_stats.ErrViewerPcapWriteID] = 2

	sub := w.Subscriber()
	sub(period, vp9_stats.VideoQualitySample{
		Nickname:      "viewer-1",
		SmoothBitrate: 3500000,
		SmoothFPS:     25.0,
	})

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "bench-callzip-node-01-0.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var s Sample
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(s.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(s.Errors))
	}
	if s.Errors["vp9_stats.Publisher.ErrViewerSeqNoJumpID"] != 5 {
		t.Fatalf("ErrViewerSeqNoJumpID = %d, want 5", s.Errors["vp9_stats.Publisher.ErrViewerSeqNoJumpID"])
	}
	if s.Errors["vp9_stats.Publisher.ErrViewerPcapWriteID"] != 2 {
		t.Fatalf("ErrViewerPcapWriteID = %d, want 2", s.Errors["vp9_stats.Publisher.ErrViewerPcapWriteID"])
	}
}

func TestSubscriberOmitsEmptyErrors(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "node-01", 0)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sub := w.Subscriber()
	sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
		Nickname:      "viewer-1",
		SmoothBitrate: 3500000,
		SmoothFPS:     25.0,
	})

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "bench-callzip-node-01-0.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	line := strings.TrimSpace(string(data))
	if strings.Contains(line, `"errors"`) {
		t.Fatalf("expected no errors key in JSON when no errors, got: %s", line)
	}
}

func TestSubscriberConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "concurrent-host", 1)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sub := w.Subscriber()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub(vp9_stats.Period{}, vp9_stats.VideoQualitySample{
				Nickname:      "viewer",
				SmoothBitrate: 3500000,
				SmoothFPS:     25,
			})
		}()
	}
	wg.Wait()

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "bench-callzip-concurrent-host-1.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 100 {
		t.Fatalf("expected 100 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var s Sample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatalf("unmarshal line %d: %v", i, err)
		}
	}
}

func TestInstanceInFilename(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "myhost", 42)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w.Close()

	path := filepath.Join(dir, "bench-callzip-myhost-42.jsonl")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected file %s to exist", path)
	}
}
