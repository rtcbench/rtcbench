package statsjsonl

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"call.zip/pkg/vp9_stats"
)

// Sample is the JSON structure written to the JSONL file.
type Sample struct {
	Timestamp  float64 `json:"ts"`
	Client     string  `json:"client"`
	Host       string  `json:"host"`
	Instance   int     `json:"instance"`
	Viewer     string  `json:"viewer"`
	BitrateBps float64 `json:"bitrate_bps"`
	FPS        float64 `json:"fps"`
}

// Writer writes JSONL stats samples to a file.
type Writer struct {
	mu       sync.Mutex
	file     *os.File
	host     string
	instance int
	enc      *json.Encoder
}

// New creates a Writer that writes to {dir}/bench-callzip-{host}-{instance}.jsonl.
func New(dir string, host string, instance int) (*Writer, error) {
	path := fmt.Sprintf("%s/bench-callzip-%s-%d.jsonl", dir, host, instance)
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("statsjsonl: create %s: %w", path, err)
	}
	return &Writer{
		file:     f,
		host:     host,
		instance: instance,
		enc:      json.NewEncoder(f),
	}, nil
}

// Subscriber returns a function compatible with vp9_stats.Publisher.AddSubscriber.
func (w *Writer) Subscriber() func(vp9_stats.Period, vp9_stats.VideoQualitySample) {
	return func(_ vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		s := Sample{
			Timestamp:  float64(time.Now().UnixMicro()) / 1e6,
			Client:     "callzip",
			Host:       w.host,
			Instance:   w.instance,
			Viewer:     sample.Nickname,
			BitrateBps: float64(sample.SmoothBitrate),
			FPS:        float64(sample.SmoothFPS),
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		w.enc.Encode(s)
	}
}

// Close flushes and closes the underlying file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
