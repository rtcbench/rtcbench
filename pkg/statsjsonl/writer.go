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
	Timestamp           float64          `json:"ts"`
	Client              string           `json:"client"`
	Host                string           `json:"host"`
	Instance            int              `json:"instance"`
	Viewer              string           `json:"viewer"`
	BitrateBps          float64          `json:"bitrate_bps"`
	FPS                 float64          `json:"fps"`
	DecoderSmoothFPS    float64          `json:"dec_sm_fps"`
	DecoderBufferFPS    float64          `json:"dec_buf_fps"`
	EstimatedDecoderFPS int              `json:"est_dec_fps"`
	FrameJitterUS       float64          `json:"frame_jitter_us"`
	PacketJitterUS      float64          `json:"packet_jitter_us"`
	FramesComplete      int64            `json:"frames_complete"`
	FramesLost          int64            `json:"frames_lost"`
	PLISent             int64            `json:"pli_sent"`
	MaxRecvSID          uint8            `json:"max_recv_sid"`
	MaxRecvTID          uint8            `json:"max_recv_tid"`
	Errors              map[string]int32 `json:"errors,omitempty"`
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
	return func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		var errMap map[string]int32
		for i, count := range period.Errors {
			if count > 0 {
				if errMap == nil {
					errMap = make(map[string]int32)
				}
				errMap[vp9_stats.ErrIDToString(i)] = count
			}
		}
		s := Sample{
			Timestamp:           float64(time.Now().UnixMicro()) / 1e6,
			Client:              "callzip",
			Host:                w.host,
			Instance:            w.instance,
			Viewer:              sample.Nickname,
			BitrateBps:          float64(sample.SmoothBitrate),
			FPS:                 float64(sample.SmoothFPS),
			DecoderSmoothFPS:    float64(sample.DecoderSmoothFPS),
			DecoderBufferFPS:    float64(sample.DecoderBufferFPS),
			EstimatedDecoderFPS: sample.EstimatedDecoderFPS,
			FrameJitterUS:       sample.FrameJitterUS,
			PacketJitterUS:      sample.PacketJitterUS,
			FramesComplete:      sample.FramesComplete,
			FramesLost:          sample.FramesLost,
			PLISent:             sample.RTCP.PLISent,
			MaxRecvSID:          sample.SVC.MaxRecvSID,
			MaxRecvTID:          sample.SVC.MaxRecvTID,
			Errors:              errMap,
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
