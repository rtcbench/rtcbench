package ivf

import (
	"context"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/rtcbench/rtcbench/pkg/log"
)

// LoopIntoTrack continuously reads frames from src and writes them into
// track, pacing by IVF timestamps. It uses wall-clock-relative timing
// to prevent accumulated drift across frames.
// It runs forever; call from a goroutine.
func LoopIntoTrack(
	l *log.Logger,
	track *webrtc.TrackLocalStaticSample,
	src FrameSource,
) {
	LoopIntoTrackUntil(context.Background(), l, track, src)
}

func LoopIntoTrackUntil(
	ctx context.Context,
	l *log.Logger,
	track *webrtc.TrackLocalStaticSample,
	src FrameSource,
) {
	nextTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, dur, err := src.NextFrame()
		if err != nil {
			l.Errorf("[ivf] NextFrame: %v", err)
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			nextTime = time.Now()
			continue
		}

		if err := track.WriteSample(media.Sample{Data: data, Duration: dur}); err != nil {
			l.Errorf("[ivf] WriteSample: %v", err)
			return
		}

		nextTime = nextTime.Add(dur)
		if wait := time.Until(nextTime); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
