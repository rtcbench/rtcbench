package log

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Handler processes log records.
type Handler interface {
	Handle(Record)
	Close() error
}

// ANSI color codes.
const (
	colorReset = "\033[0m"
	colorCyan  = "\033[36m"
	colorGreen = "\033[32m"
	colorRed   = "\033[31m"
)

func levelColor(l Level) string {
	switch l {
	case LevelDebug:
		return colorCyan
	case LevelInfo:
		return colorGreen
	case LevelError:
		return colorRed
	default:
		return ""
	}
}

// ConsoleHandler writes formatted log lines to an io.Writer (typically os.Stderr).
type ConsoleHandler struct {
	w        io.Writer
	color    bool
	minLevel Level
	mu       sync.Mutex
}

// NewConsoleHandler creates a handler that writes to os.Stderr.
// Only records at or above minLevel are written.
func NewConsoleHandler(color bool, minLevel Level) *ConsoleHandler {
	return &ConsoleHandler{w: os.Stderr, color: color, minLevel: minLevel}
}

// NewConsoleHandlerWriter creates a handler that writes to the given writer.
// Useful for testing.
func NewConsoleHandlerWriter(w io.Writer, color bool, minLevel Level) *ConsoleHandler {
	return &ConsoleHandler{w: w, color: color, minLevel: minLevel}
}

func (h *ConsoleHandler) Handle(r Record) {
	if r.Level < h.minLevel {
		return
	}
	ts := r.Time.UTC().Format("2006-01-02T15:04:05.000000Z")
	var line string
	if h.color {
		c := levelColor(r.Level)
		line = fmt.Sprintf("%s %s%-5s%s [%s]%s %s\n", ts, c, r.Level, colorReset, r.Stream, r.Prefix, r.Msg)
	} else {
		line = fmt.Sprintf("%s %-5s [%s]%s %s\n", ts, r.Level, r.Stream, r.Prefix, r.Msg)
	}
	h.mu.Lock()
	fmt.Fprint(h.w, line)
	h.mu.Unlock()
}

func (h *ConsoleHandler) Close() error { return nil }

// FileHandler writes log records to per-stream files in a directory.
// Files are created lazily on the first record for each stream.
// If combined is true, all records are also written to combined.log.
type FileHandler struct {
	dir      string
	minLevel Level
	combined bool
	mu       sync.Mutex
	files    map[string]*os.File
}

// NewFileHandler creates a file handler that writes to {dir}/{stream}.log files.
// Only records at or above minLevel are written. If combined is true, all
// records are also appended to combined.log.
func NewFileHandler(dir string, minLevel Level, combined bool) (*FileHandler, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory %q: %w", dir, err)
	}
	return &FileHandler{
		dir:      dir,
		minLevel: minLevel,
		combined: combined,
		files:    make(map[string]*os.File),
	}, nil
}

func (h *FileHandler) Handle(r Record) {
	if r.Level < h.minLevel {
		return
	}

	ts := r.Time.UTC().Format("2006-01-02T15:04:05.000000Z")
	line := fmt.Sprintf("%s %-5s [%s]%s %s\n", ts, r.Level, r.Stream, r.Prefix, r.Msg)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.writeToFile(r.Stream, line)
	if h.combined {
		h.writeToFile("combined", line)
	}
}

func (h *FileHandler) writeToFile(name, line string) {
	f, ok := h.files[name]
	if !ok {
		path := filepath.Join(h.dir, name+".log")
		var err error
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "log: failed to open %s: %v\n", path, err)
			return
		}
		h.files[name] = f
	}
	fmt.Fprint(f, line)
}

func (h *FileHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var errs []error
	for name, f := range h.files {
		if err := f.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s.log: %w", name, err))
		}
	}
	h.files = make(map[string]*os.File)
	return errors.Join(errs...)
}

// CaptureHandler stores records in memory for test assertions.
type CaptureHandler struct {
	mu      sync.Mutex
	records []Record
}

func NewCaptureHandler() *CaptureHandler {
	return &CaptureHandler{}
}

func (h *CaptureHandler) Handle(r Record) {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
}

func (h *CaptureHandler) Close() error { return nil }

// Records returns a copy of all captured records.
func (h *CaptureHandler) Records() []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Record, len(h.records))
	copy(out, h.records)
	return out
}

// Filter returns records matching the given stream and minimum level.
func (h *CaptureHandler) Filter(stream string, level Level) []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Record
	for _, r := range h.records {
		if r.Stream == stream && r.Level >= level {
			out = append(out, r)
		}
	}
	return out
}

// Reset clears all captured records.
func (h *CaptureHandler) Reset() {
	h.mu.Lock()
	h.records = nil
	h.mu.Unlock()
}
