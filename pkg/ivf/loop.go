package ivf

import (
	"os"
	"time"

	"call.zip/pkg/cc"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfreader"
)

// LoopIntoTrack continuously re-reads the IVF files in paths (cycling in order)
// and writes VP9 frames into track, pacing by the IVF timestamps.
// It runs forever; call from a goroutine.
//
// If ctrl is non-nil, the sleep between frames is scaled up when the controller's
// TargetBitrate is below the frame's natural bitrate, reducing the effective send rate.
func LoopIntoTrack(
	logf func(string, ...any),
	track *webrtc.TrackLocalStaticSample,
	paths []string,
	ctrl cc.Controller,
) {
	defaultDur := 33 * time.Millisecond
	idx := 0

	for {
		f, err := os.Open(paths[idx])
		if err != nil {
			logf("[ivf] open %s: %v", paths[idx], err)
			time.Sleep(2 * time.Second)
			continue
		}

		r, header, err := ivfreader.NewWith(f)
		if err != nil {
			_ = f.Close()
			logf("[ivf] NewWith: %v", err)
			time.Sleep(2 * time.Second)
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

			// Scale sleep up if the CC controller has set a target below the natural rate.
			sleep := dur
			if ctrl != nil {
				if target := ctrl.TargetBitrate(); target > 0 {
					naturalBps := uint64(len(frame)) * 8 * uint64(time.Second) / uint64(dur)
					if target < naturalBps {
						sleep = time.Duration(float64(dur) * float64(naturalBps) / float64(target))
					}
				}
			}

			if err := track.WriteSample(media.Sample{Data: frame, Duration: dur}); err != nil {
				logf("[ivf] WriteSample: %v", err)
				_ = f.Close()
				return
			}
			time.Sleep(sleep)
		}

		_ = f.Close()
		idx = (idx + 1) % len(paths)
	}
}
