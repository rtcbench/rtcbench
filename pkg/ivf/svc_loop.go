package ivf

import (
	"context"
	"time"

	"github.com/pion/rtp"
	vp9hdr "github.com/pion/rtp/codecs/vp9"
	"github.com/pion/webrtc/v4"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/vp9"
)

// SVCConfig describes the SVC layer structure for the send loop.
type SVCConfig struct {
	NumSpatialLayers  int
	NumTemporalLayers int
	Widths            []uint16 // resolution per spatial layer
	Heights           []uint16 // resolution per spatial layer
	TargetBitrateBps  int      // encoding bitrate for the full layer set (used for layer selection)
}

const svcMTU = 1200

// TemporalLayerID returns the temporal layer ID for a given frame index
// using standard dyadic temporal layering.
//
//	1 TL: all TID=0
//	2 TL: [0, 1] repeating
//	3 TL: [0, 2, 1, 2] repeating
func TemporalLayerID(frameIdx, numTemporalLayers int) uint8 {
	switch numTemporalLayers {
	case 1:
		return 0
	case 2:
		if frameIdx%2 == 0 {
			return 0
		}
		return 1
	default: // 3+
		switch frameIdx % 4 {
		case 0:
			return 0
		case 2:
			return 1
		default:
			return 2
		}
	}
}

// SVCLoopIntoTrack continuously reads frames from src, demuxes SVC
// superframes, and writes RTP packets with proper SVC layer indices
// into track. It runs forever; call from a goroutine.
//
// targetBitrate optionally returns the current bandwidth estimate in bps.
// When non-nil, higher layers are dropped when bandwidth is constrained.
func SVCLoopIntoTrack(
	l *log.Logger,
	track *webrtc.TrackLocalStaticRTP,
	src FrameSource,
	cfg SVCConfig,
	targetBitrate func() int, // may be nil
) {
	SVCLoopIntoTrackUntil(context.Background(), l, track, src, cfg, targetBitrate)
}

func SVCLoopIntoTrackUntil(
	ctx context.Context,
	l *log.Logger,
	track *webrtc.TrackLocalStaticRTP,
	src FrameSource,
	cfg SVCConfig,
	targetBitrate func() int, // may be nil
) {
	payloader := &vp9.SVCPayloader{
		Config: vp9.SVCLayerConfig{
			NumSpatialLayers:  cfg.NumSpatialLayers,
			NumTemporalLayers: cfg.NumTemporalLayers,
			Widths:            cfg.Widths,
			Heights:           cfg.Heights,
		},
	}

	var seqNo uint16
	var rtpTS uint32
	frameIdx := 0
	nextTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, dur, err := src.NextFrame()
		if err != nil {
			l.Errorf("[ivf-svc] NextFrame: %v", err)
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

		tid := TemporalLayerID(frameIdx, cfg.NumTemporalLayers)

		subFrames, err := vp9.ParseSuperframe(data)
		if err != nil {
			l.Errorf("[ivf-svc] ParseSuperframe: %v", err)
			time.Sleep(2 * time.Second)
			nextTime = time.Now()
			continue
		}

		// Determine max layers from bandwidth estimate
		maxSID := uint8(cfg.NumSpatialLayers - 1)
		maxTID := uint8(cfg.NumTemporalLayers - 1)
		if targetBitrate != nil {
			maxSID, maxTID = selectLayers(targetBitrate(), cfg)
		}

		// Skip entire frame if TID exceeds budget
		if tid > maxTID {
			rtpTS += durToRTPTicks(dur)
			frameIdx++
			if !sleepUntil(ctx, &nextTime, dur) {
				return
			}
			continue
		}

		// Collect all RTP packets for the selected layers first, then send
		// them paced evenly over the frame interval. This avoids bursting
		// all packets at once — which causes tc to spike queuing delay and
		// inflates interarrival jitter (same role as Chrome's PacedSender).
		var allPkts []*rtp.Packet
		for _, sf := range subFrames {
			sid := uint8(sf.Index)
			if sid > maxSID {
				continue
			}

			isKeyframe := isRawKeyframe(sf.Data)
			isLastSL := sid >= maxSID || sf.Index == len(subFrames)-1

			payloads := payloader.PayloadSVC(svcMTU, sf.Data, vp9.SVCFrameInfo{
				SID:        sid,
				TID:        tid,
				IsKeyframe: isKeyframe,
			})

			for i, payload := range payloads {
				pkt := &rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						SequenceNumber: seqNo,
						Timestamp:      rtpTS,
						Marker:         i == len(payloads)-1 && isLastSL,
					},
					Payload: payload,
				}
				allPkts = append(allPkts, pkt)
				seqNo++
			}
		}

		n := len(allPkts)
		for i, pkt := range allPkts {
			if n > 1 {
				// Space packet i at nextTime + i*(dur/n), distributing
				// sends evenly over [nextTime, nextTime+dur). The tail
				// of the interval is consumed by sleepUntil below.
				deadline := nextTime.Add(time.Duration(i) * dur / time.Duration(n))
				if wait := time.Until(deadline); wait > 0 {
					timer := time.NewTimer(wait)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}
			if err := track.WriteRTP(pkt); err != nil {
				l.Errorf("[ivf-svc] WriteRTP: %v", err)
				return
			}
		}

		payloader.AdvancePicture(tid)

		rtpTS += durToRTPTicks(dur)
		frameIdx++
		if !sleepUntil(ctx, &nextTime, dur) {
			return
		}
	}
}

// isRawKeyframe parses a VP9 frame header to determine if the frame is a keyframe.
func isRawKeyframe(data []byte) bool {
	var hdr vp9hdr.Header
	if err := hdr.Unmarshal(data); err != nil {
		return false
	}
	return !hdr.NonKeyFrame
}

// durToRTPTicks converts a duration to 90kHz RTP clock ticks.
func durToRTPTicks(d time.Duration) uint32 {
	return uint32(d.Seconds() * 90000)
}

// sleepUntil advances nextTime by dur and sleeps until that time.
func sleepUntil(ctx context.Context, nextTime *time.Time, dur time.Duration) bool {
	*nextTime = nextTime.Add(dur)
	if wait := time.Until(*nextTime); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return true
}

// layerBitrateThreshold returns the cumulative bitrate (bps) needed to send
// up to spatial layer sid and temporal layer tid, given a total target for the
// full layer set. The fractions approximate typical VP9 SVC allocation.
//
// Spatial distribution (3 layers): S0=6%, S1=23%, S2=71%
// Temporal distribution (3 layers): T0=33%, T1=33%, T2=34%
// Temporal distribution (2 layers): T0=50%, T1=50%
func layerBitrateThreshold(sid, tid uint8, cfg SVCConfig) int {
	totalBps := cfg.TargetBitrateBps
	if totalBps <= 0 {
		return 0
	}

	// Spatial cumulative fractions
	var spatialFrac float64
	switch cfg.NumSpatialLayers {
	case 1:
		spatialFrac = 1.0
	case 2:
		spatialFrac = [2]float64{0.25, 1.0}[sid]
	default: // 3
		spatialFrac = [3]float64{0.06, 0.29, 1.0}[min(int(sid), 2)]
	}

	// Temporal cumulative fractions within the spatial layer
	var temporalFrac float64
	switch cfg.NumTemporalLayers {
	case 1:
		temporalFrac = 1.0
	case 2:
		temporalFrac = [2]float64{0.50, 1.0}[min(int(tid), 1)]
	default: // 3
		temporalFrac = [3]float64{0.33, 0.66, 1.0}[min(int(tid), 2)]
	}

	return int(float64(totalBps) * spatialFrac * temporalFrac)
}

// selectLayers determines the max SID and TID based on available bandwidth.
// It compares the GCC bandwidth estimate against the encoding target bitrate
// to decide which layers can be afforded. Walks down from the highest layer
// combination until one fits within the available bandwidth.
// A 10% hysteresis margin avoids rapid oscillation at boundary bitrates.
func selectLayers(bitrateBps int, cfg SVCConfig) (maxSID, maxTID uint8) {
	maxSID = uint8(cfg.NumSpatialLayers - 1)
	maxTID = uint8(cfg.NumTemporalLayers - 1)

	if bitrateBps <= 0 {
		return 0, 0
	}

	// If no target bitrate configured, send all layers.
	if cfg.TargetBitrateBps <= 0 {
		return maxSID, maxTID
	}

	// Walk from highest to lowest. Drop temporal layers first, then spatial.
	for sid := int(maxSID); sid >= 0; sid-- {
		for tid := int(maxTID); tid >= 0; tid-- {
			threshold := layerBitrateThreshold(uint8(sid), uint8(tid), cfg)
			// 90% of threshold to add hysteresis (don't drop layers on minor dips)
			if bitrateBps >= int(float64(threshold)*0.9) {
				return uint8(sid), uint8(tid)
			}
		}
	}
	return 0, 0
}
