package ivf

import (
	"os"
	"time"

	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfreader"

	"github.com/pion/webrtc/v3"
)

// LoopIntoTrack continuously re-reads the IVF files in paths (cycling in order)
// and writes VP9 frames into track, pacing by the IVF timestamps.
// It uses wall-clock-relative timing to prevent accumulated drift.
// It runs forever; call from a goroutine.
func LoopIntoTrack(
	logf func(string, ...any),
	track *webrtc.TrackLocalStaticSample,
	paths []string,
) {
	defaultDur := 33 * time.Millisecond
	idx := 0
	nextTime := time.Now()

	for {
		f, err := os.Open(paths[idx])
		if err != nil {
			logf("[ivf] open %s: %v", paths[idx], err)
			time.Sleep(2 * time.Second)
			nextTime = time.Now()
			continue
		}

		r, header, err := ivfreader.NewWith(f)
		if err != nil {
			_ = f.Close()
			logf("[ivf] NewWith: %v", err)
			time.Sleep(2 * time.Second)
			nextTime = time.Now()
			continue
		}

		num := header.TimebaseNumerator
		den := header.TimebaseDenominator
		var prevTS *uint64

		for {
			frame, fh, err := r.ParseNextFrame()
			if err != nil {
				logf("[ivf] ParseNextFrame: %v", err)
				break
			}
			if frame == nil && fh == nil {
				break
			}

			dur := defaultDur
			if prevTS != nil && fh.Timestamp > *prevTS && num != 0 && den != 0 {
				ticks := fh.Timestamp - *prevTS
				dur = time.Duration(int64(time.Second) * int64(ticks) * int64(num) / int64(den))
				if dur <= 0 {
					dur = defaultDur
				}
			}
			ts := fh.Timestamp
			prevTS = &ts

			if err := track.WriteSample(media.Sample{Data: frame, Duration: dur}); err != nil {
				logf("[ivf] WriteSample: %v", err)
				_ = f.Close()
				return
			}

			nextTime = nextTime.Add(dur)
			if wait := time.Until(nextTime); wait > 0 {
				time.Sleep(wait)
			}
		}

		_ = f.Close()
		idx = (idx + 1) % len(paths)
	}
}
