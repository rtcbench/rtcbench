package metricsserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

type jsonlSample struct {
	Timestamp           float64          `json:"ts"`
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

func Replay(r io.Reader, start time.Time) (*Server, error) {
	s := New(0)
	if !start.IsZero() {
		s.Record(start, Thresholds{})
	}
	errorIDs := make(map[string]int, vp9_stats.NumErrIDs)
	for i := range vp9_stats.NumErrIDs {
		errorIDs[vp9_stats.ErrIDToString(i)] = i
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var last time.Time
	var errs [vp9_stats.NumErrIDs]int32
	for scanner.Scan() {
		var line jsonlSample
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil || line.Viewer == "" || line.Timestamp <= 0 {
			continue
		}
		at := time.UnixMicro(int64(line.Timestamp * 1e6))
		if s.rec == nil {
			s.Record(at, Thresholds{})
		}
		if at.After(last) {
			last = at
		}
		for name, count := range line.Errors {
			if id, ok := errorIDs[name]; ok {
				errs[id] = max(errs[id], count)
			}
		}
		period := vp9_stats.Period{Errors: errs}
		s.Tick(at)
		s.observe(at, period, vp9_stats.VideoQualitySample{
			Nickname:            line.Viewer,
			SmoothFPS:           float32(line.FPS),
			SmoothBitrate:       float32(line.BitrateBps),
			DecoderSmoothFPS:    float32(line.DecoderSmoothFPS),
			DecoderBufferFPS:    float32(line.DecoderBufferFPS),
			EstimatedDecoderFPS: line.EstimatedDecoderFPS,
			FrameJitterUS:       line.FrameJitterUS,
			PacketJitterUS:      line.PacketJitterUS,
			FramesComplete:      line.FramesComplete,
			FramesLost:          line.FramesLost,
			SVC:                 vp9_stats.SVCData{MaxRecvSID: line.MaxRecvSID, MaxRecvTID: line.MaxRecvTID},
			RTCP:                vp9_stats.RTCPData{PLISent: line.PLISent},
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("metricsserver: replay: %w", err)
	}
	if s.rec != nil && !last.IsZero() {
		s.Tick(last.Add(s.rec.interval))
	}
	return s, nil
}
