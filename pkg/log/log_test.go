package log

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
		err   bool
	}{
		{"debug", LevelDebug, false},
		{"info", LevelInfo, false},
		{"error", LevelError, false},
		{"DEBUG", 0, true},
		{"warn", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseLevel(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParseLevel(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelError, "ERROR"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestLevelFiltering(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"general": LevelInfo})
	l := reg.NewLogger("general", "[test]")

	l.Debugf("should be filtered")
	l.Infof("should pass")
	l.Errorf("should also pass")

	recs := cap.Records()
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Level != LevelInfo {
		t.Errorf("record[0] level = %v, want INFO", recs[0].Level)
	}
	if recs[1].Level != LevelError {
		t.Errorf("record[1] level = %v, want ERROR", recs[1].Level)
	}
}

func TestLevelFilteringDebugStream(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"signaling": LevelDebug})
	l := reg.NewLogger("signaling", "[test]")

	l.Debugf("debug msg")
	l.Infof("info msg")

	recs := cap.Records()
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
}

func TestUnknownStreamDefaultsToInfo(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{})
	l := reg.NewLogger("unknown-stream", "")

	l.Debugf("should be filtered")
	l.Infof("should pass")

	recs := cap.Records()
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}

func TestMultiHandlerFanOut(t *testing.T) {
	cap1 := NewCaptureHandler()
	cap2 := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap1, cap2}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "")

	l.Infof("hello")

	if len(cap1.Records()) != 1 {
		t.Fatalf("cap1: expected 1 record, got %d", len(cap1.Records()))
	}
	if len(cap2.Records()) != 1 {
		t.Fatalf("cap2: expected 1 record, got %d", len(cap2.Records()))
	}
}

func TestLoggerWith(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"general": LevelDebug})
	base := reg.NewLogger("general", "[janus]")
	child := base.With("[sender-abc]")

	child.Infof("hello")

	recs := cap.Records()
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Prefix != "[janus][sender-abc]" {
		t.Errorf("prefix = %q, want %q", recs[0].Prefix, "[janus][sender-abc]")
	}
	if recs[0].Stream != "general" {
		t.Errorf("stream = %q, want %q", recs[0].Stream, "general")
	}
}

func TestLoggerWithDoesNotMutateParent(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"general": LevelDebug})
	parent := reg.NewLogger("general", "[parent]")
	_ = parent.With("[child]")

	parent.Infof("from parent")

	recs := cap.Records()
	if recs[0].Prefix != "[parent]" {
		t.Errorf("parent prefix = %q, want %q", recs[0].Prefix, "[parent]")
	}
}

func TestInfofSignatureCompatibility(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "")

	// This must compile: Logger.Infof is assignable to func(string, ...any).
	var logf func(string, ...any) = l.Infof
	logf("hello %s", "world")

	recs := cap.Records()
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Msg != "hello world" {
		t.Errorf("msg = %q, want %q", recs[0].Msg, "hello world")
	}
}

func TestRecordFields(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"signaling": LevelDebug})
	l := reg.NewLogger("signaling", "[jitsi][user-1]")

	l.Debugf("step %d done", 3)

	recs := cap.Records()
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	r := recs[0]
	if r.Level != LevelDebug {
		t.Errorf("level = %v, want DEBUG", r.Level)
	}
	if r.Stream != "signaling" {
		t.Errorf("stream = %q, want %q", r.Stream, "signaling")
	}
	if r.Prefix != "[jitsi][user-1]" {
		t.Errorf("prefix = %q, want %q", r.Prefix, "[jitsi][user-1]")
	}
	if r.Msg != "step 3 done" {
		t.Errorf("msg = %q, want %q", r.Msg, "step 3 done")
	}
	if r.Time.IsZero() {
		t.Error("time should not be zero")
	}
}

func TestCaptureHandlerFilter(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{
		"general":    LevelDebug,
		"signaling":  LevelDebug,
		"video_stats": LevelDebug,
	})

	reg.NewLogger("general", "").Infof("gen info")
	reg.NewLogger("general", "").Debugf("gen debug")
	reg.NewLogger("signaling", "").Errorf("sig error")
	reg.NewLogger("video_stats", "").Infof("stats info")

	if got := len(cap.Filter("general", LevelDebug)); got != 2 {
		t.Errorf("Filter(general, debug) = %d, want 2", got)
	}
	if got := len(cap.Filter("general", LevelInfo)); got != 1 {
		t.Errorf("Filter(general, info) = %d, want 1", got)
	}
	if got := len(cap.Filter("signaling", LevelError)); got != 1 {
		t.Errorf("Filter(signaling, error) = %d, want 1", got)
	}
	if got := len(cap.Filter("video_stats", LevelDebug)); got != 1 {
		t.Errorf("Filter(video_stats, debug) = %d, want 1", got)
	}
}

func TestCaptureHandlerReset(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "")

	l.Infof("first")
	cap.Reset()
	l.Infof("second")

	recs := cap.Records()
	if len(recs) != 1 {
		t.Fatalf("expected 1 record after reset, got %d", len(recs))
	}
	if recs[0].Msg != "second" {
		t.Errorf("msg = %q, want %q", recs[0].Msg, "second")
	}
}

func TestConsoleHandlerNoColor(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandlerWriter(&buf, false, LevelDebug)
	reg := NewRegistry([]Handler{h}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "[test]")

	l.Infof("hello")

	line := buf.String()
	if strings.Contains(line, "\033[") {
		t.Errorf("no-color output contains ANSI codes: %q", line)
	}
	if !strings.Contains(line, "INFO") {
		t.Errorf("output missing INFO: %q", line)
	}
	if !strings.Contains(line, "[general]") {
		t.Errorf("output missing stream: %q", line)
	}
	if !strings.Contains(line, "[test]") {
		t.Errorf("output missing prefix: %q", line)
	}
	if !strings.Contains(line, "hello") {
		t.Errorf("output missing message: %q", line)
	}
}

func TestConsoleHandlerFormat(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandlerWriter(&buf, false, LevelDebug)
	reg := NewRegistry([]Handler{h}, map[string]Level{"janus": LevelDebug})
	l := reg.NewLogger("janus", "[sender][user-1]")

	l.Infof("connected")

	line := buf.String()
	// UTC ISO 8601 timestamp with milliseconds
	if !strings.Contains(line, "T") || !strings.Contains(line, "Z") {
		t.Errorf("output missing UTC ISO timestamp: %q", line)
	}
	// Stream name in brackets
	if !strings.Contains(line, "[janus]") {
		t.Errorf("output missing stream name: %q", line)
	}
	// Prefix directly after stream
	if !strings.Contains(line, "[janus][sender][user-1]") {
		t.Errorf("output missing stream+prefix: %q", line)
	}
}

func TestConsoleHandlerColor(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandlerWriter(&buf, true, LevelDebug)
	reg := NewRegistry([]Handler{h}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "")

	l.Infof("green")
	l.Debugf("cyan")
	l.Errorf("red")

	out := buf.String()
	if !strings.Contains(out, colorGreen) {
		t.Error("INFO line missing green color code")
	}
	if !strings.Contains(out, colorCyan) {
		t.Error("DEBUG line missing cyan color code")
	}
	if !strings.Contains(out, colorRed) {
		t.Error("ERROR line missing red color code")
	}
	if !strings.Contains(out, colorReset) {
		t.Error("output missing reset color code")
	}
}

func TestFileHandler(t *testing.T) {
	dir := t.TempDir()
	fh, err := NewFileHandler(dir, LevelDebug, false)
	if err != nil {
		t.Fatalf("NewFileHandler: %v", err)
	}

	reg := NewRegistry([]Handler{fh}, map[string]Level{
		"general":   LevelDebug,
		"signaling": LevelDebug,
	})

	reg.NewLogger("general", "[main]").Infof("hello general")
	reg.NewLogger("signaling", "[jitsi]").Debugf("hello signaling")

	if err := fh.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	genContent, err := os.ReadFile(filepath.Join(dir, "general.log"))
	if err != nil {
		t.Fatalf("read general.log: %v", err)
	}
	if !strings.Contains(string(genContent), "hello general") {
		t.Errorf("general.log missing message: %s", genContent)
	}
	if !strings.Contains(string(genContent), "[main]") {
		t.Errorf("general.log missing prefix: %s", genContent)
	}

	sigContent, err := os.ReadFile(filepath.Join(dir, "signaling.log"))
	if err != nil {
		t.Fatalf("read signaling.log: %v", err)
	}
	if !strings.Contains(string(sigContent), "hello signaling") {
		t.Errorf("signaling.log missing message: %s", sigContent)
	}
}

func TestFileHandlerNoColorCodes(t *testing.T) {
	dir := t.TempDir()
	fh, err := NewFileHandler(dir, LevelDebug, false)
	if err != nil {
		t.Fatalf("NewFileHandler: %v", err)
	}
	reg := NewRegistry([]Handler{fh}, map[string]Level{"general": LevelDebug})
	reg.NewLogger("general", "").Infof("test")
	fh.Close()

	content, _ := os.ReadFile(filepath.Join(dir, "general.log"))
	if strings.Contains(string(content), "\033[") {
		t.Errorf("file output contains ANSI codes: %s", content)
	}
}

func TestRegistryClose(t *testing.T) {
	dir := t.TempDir()
	fh, err := NewFileHandler(dir, LevelDebug, false)
	if err != nil {
		t.Fatalf("NewFileHandler: %v", err)
	}
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{fh, cap}, map[string]Level{})

	if err := reg.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestConsoleHandlerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandlerWriter(&buf, false, LevelInfo)
	reg := NewRegistry([]Handler{h}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "")

	l.Debugf("debug msg")
	l.Infof("info msg")
	l.Errorf("error msg")

	out := buf.String()
	if strings.Contains(out, "debug msg") {
		t.Error("console with minLevel=Info should not contain debug messages")
	}
	if !strings.Contains(out, "info msg") {
		t.Error("console should contain info messages")
	}
	if !strings.Contains(out, "error msg") {
		t.Error("console should contain error messages")
	}
}

func TestFileHandlerLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	fh, err := NewFileHandler(dir, LevelError, false)
	if err != nil {
		t.Fatalf("NewFileHandler: %v", err)
	}
	reg := NewRegistry([]Handler{fh}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "")

	l.Debugf("debug msg")
	l.Infof("info msg")
	l.Errorf("error msg")
	fh.Close()

	content, _ := os.ReadFile(filepath.Join(dir, "general.log"))
	s := string(content)
	if strings.Contains(s, "debug msg") {
		t.Error("file with minLevel=Error should not contain debug messages")
	}
	if strings.Contains(s, "info msg") {
		t.Error("file with minLevel=Error should not contain info messages")
	}
	if !strings.Contains(s, "error msg") {
		t.Error("file should contain error messages")
	}
}

func TestFileHandlerCombined(t *testing.T) {
	dir := t.TempDir()
	fh, err := NewFileHandler(dir, LevelDebug, true)
	if err != nil {
		t.Fatalf("NewFileHandler: %v", err)
	}
	reg := NewRegistry([]Handler{fh}, map[string]Level{
		"general":   LevelDebug,
		"signaling": LevelDebug,
	})

	reg.NewLogger("general", "").Infof("gen msg")
	reg.NewLogger("signaling", "").Infof("sig msg")
	fh.Close()

	// Per-stream files should exist
	genContent, err := os.ReadFile(filepath.Join(dir, "general.log"))
	if err != nil {
		t.Fatalf("read general.log: %v", err)
	}
	if !strings.Contains(string(genContent), "gen msg") {
		t.Error("general.log missing message")
	}

	sigContent, err := os.ReadFile(filepath.Join(dir, "signaling.log"))
	if err != nil {
		t.Fatalf("read signaling.log: %v", err)
	}
	if !strings.Contains(string(sigContent), "sig msg") {
		t.Error("signaling.log missing message")
	}

	// Combined file should have both
	combContent, err := os.ReadFile(filepath.Join(dir, "combined.log"))
	if err != nil {
		t.Fatalf("read combined.log: %v", err)
	}
	cs := string(combContent)
	if !strings.Contains(cs, "gen msg") {
		t.Error("combined.log missing general message")
	}
	if !strings.Contains(cs, "sig msg") {
		t.Error("combined.log missing signaling message")
	}
}

func TestFileHandlerNoCombinedByDefault(t *testing.T) {
	dir := t.TempDir()
	fh, err := NewFileHandler(dir, LevelDebug, false)
	if err != nil {
		t.Fatalf("NewFileHandler: %v", err)
	}
	reg := NewRegistry([]Handler{fh}, map[string]Level{"general": LevelDebug})
	reg.NewLogger("general", "").Infof("test")
	fh.Close()

	if _, err := os.Stat(filepath.Join(dir, "combined.log")); err == nil {
		t.Error("combined.log should not exist when combined=false")
	}
}

func TestConcurrentWrites(t *testing.T) {
	cap := NewCaptureHandler()
	reg := NewRegistry([]Handler{cap}, map[string]Level{"general": LevelDebug})
	l := reg.NewLogger("general", "")

	const goroutines = 10
	const writes = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range writes {
				l.Infof("g=%d w=%d", id, j)
			}
		}(i)
	}
	wg.Wait()

	recs := cap.Records()
	if len(recs) != goroutines*writes {
		t.Errorf("expected %d records, got %d", goroutines*writes, len(recs))
	}
}
