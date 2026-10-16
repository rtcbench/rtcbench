package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
)

type Quality struct {
	Expected   int     `json:"expected"`
	Reported   int     `json:"reported"`
	Healthy    int     `json:"healthy"`
	Fraction   float64 `json:"fraction"`
	MinFPS     float64 `json:"minFPS"`
	MinBitrate float64 `json:"minBitrate"`
	Passed     bool    `json:"passed"`
	Reason     string  `json:"reason"`
}

type streamSample struct {
	Timestamp float64 `json:"ts"`
	Viewer    string  `json:"viewer"`
	Bitrate   float64 `json:"bitrate_bps"`
	FPS       float64 `json:"fps"`
}

func NativeQuality(reader io.Reader, expected int, cfg Cluster, since float64) (Quality, error) {
	latest := map[string]streamSample{}
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 4096), 1<<20)
	scan.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i+1], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for scan.Scan() {
		if strings.TrimSpace(scan.Text()) == "" {
			continue
		}
		var sample streamSample
		if err := json.Unmarshal(scan.Bytes(), &sample); err != nil {
			if !bytes.HasSuffix(scan.Bytes(), []byte{'\n'}) {
				if decodeErr := json.NewDecoder(bytes.NewReader(scan.Bytes())).Decode(&sample); decodeErr == io.ErrUnexpectedEOF {
					break
				}
			}
			return Quality{}, fmt.Errorf("native stats: %w", err)
		}
		if sample.Viewer == "" || sample.Timestamp < since {
			continue
		}
		if prev, ok := latest[sample.Viewer]; !ok || prev.Timestamp < sample.Timestamp {
			latest[sample.Viewer] = sample
		}
	}
	if err := scan.Err(); err != nil {
		return Quality{}, err
	}
	q := Quality{Expected: expected, Reported: len(latest), MinFPS: math.Inf(1), MinBitrate: math.Inf(1)}
	for _, sample := range latest {
		q.MinFPS = min(q.MinFPS, sample.FPS)
		q.MinBitrate = min(q.MinBitrate, sample.Bitrate)
		if sample.FPS >= cfg.MinFPS && sample.Bitrate >= cfg.MinBitrate {
			q.Healthy++
		}
	}
	if len(latest) == 0 {
		q.MinFPS, q.MinBitrate = 0, 0
	}
	return finishQuality(q, cfg.HealthyFraction), nil
}

func finishQuality(q Quality, fraction float64) Quality {
	if q.Expected > 0 {
		q.Fraction = min(1, float64(q.Healthy)/float64(q.Expected))
	}
	q.Passed = q.Expected > 0 && q.Fraction >= fraction
	switch {
	case q.Passed:
		q.Reason = "OK"
	case q.Reported == 0:
		q.Reason = "NO_STREAMS"
	case q.Reported < q.Expected:
		q.Reason = "STREAMS_MISSING"
	default:
		q.Reason = "QUALITY_DEGRADED"
	}
	return q
}
