package ivf

import (
	"time"

	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
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
	nextTime := time.Now()

	for {
		data, dur, err := src.NextFrame()
		if err != nil {
			l.Errorf("[ivf] NextFrame: %v", err)
			time.Sleep(2 * time.Second)
			nextTime = time.Now()
			continue
		}

		if err := track.WriteSample(media.Sample{Data: data, Duration: dur}); err != nil {
			l.Errorf("[ivf] WriteSample: %v", err)
			return
		}

		nextTime = nextTime.Add(dur)
		if wait := time.Until(nextTime); wait > 0 {
			time.Sleep(wait)
		}
	}
}
