package vp9

import (
	"fmt"
	"strings"
)

const (
	maxLayerSpatialID  = 2
	maxLayerTemporalID = 2

	nSpatialLayers  = maxLayerSpatialID + 1
	nTemporalLayers = maxLayerTemporalID + 1

	StatsBufferSize = 2048

	fpsEWMAAlpha = float32(0.9)
)

type FrameStatistics struct {
	// pos current position in the buffer
	pos int

	// len number of packets in the buffer
	len int

	// packets buffer of parsed frameInfo per VP9 payload
	packets [StatsBufferSize]frameInfo

	// layers histogram of SID x TID counts
	layers [nSpatialLayers][nTemporalLayers]int

	// rtpTimestamps histogram of rtpTimestamps (used for frame counting)
	rtpTimestamps map[uint32]int

	// uniqueRtpTimestamps cached count of unique packet timestamps (used as estimated count of frames)
	uniqueRtpTimestamps int

	// latestSmoothFPS latest computed smoothed frame per second value
	latestSmoothFPS float32

	// skipInvalidPackets count of packets skipped from stats due to invalid data
	skipInvalidPackets int64
}

type frameInfo struct {
	// clientReadTime packet received-at time (epoch utc microseconds)
	clientReadTime int64

	// rtpTimestamp remote timestamp (all packets of the same frame have the same timestamp)
	rtpTimestamp uint32

	// bufferFPS FPS computed using start and end time of all full frames in buffer.
	// We exclude frames at the start and end of the buffer as they may be partial.
	bufferFPS float32

	// spatialID video resolution (see PayloadDescriptor.SID)
	spatialID uint8

	// temporalID video frame rate (see PayloadDescriptor.TID)
	temporalID uint8
}

type VideoQualitySample struct {
	// Layers histogram of SID x TID counts (counted by packet)
	Layers [nSpatialLayers][nTemporalLayers]int

	// SmoothFPS latest computed smoothed frame per second value
	SmoothFPS float32
}

// AcceptPacket updates the stat tracker with information about the newly arrived VP9 RTP packet
func (stats *FrameStatistics) AcceptPacket(clientReadTime int64, rtpTimestamp uint32, payloadDesc *PayloadDescriptor) {
	spatialID := payloadDesc.SID
	temporalID := payloadDesc.TID

	if spatialID > maxLayerSpatialID || temporalID > maxLayerTemporalID {
		stats.skipInvalidPackets++
		return
	}

	evictedPacket := stats.packets[stats.pos]

	stats.packets[stats.pos] = frameInfo{
		clientReadTime: clientReadTime,
		rtpTimestamp:   rtpTimestamp,
		bufferFPS:      float32(0),
		spatialID:      spatialID,
		temporalID:     temporalID,
	}

	if stats.len < StatsBufferSize { // buffer growth phase
		stats.pos++
		stats.len++
		if stats.len == StatsBufferSize { // stats initialization
			stats.rtpTimestamps = make(map[uint32]int) // TODO reuse map

			for i := 0; i < StatsBufferSize; i++ {
				stats.layers[stats.packets[i].spatialID][stats.packets[i].temporalID]++
				if _, frameExists := stats.rtpTimestamps[stats.packets[i].rtpTimestamp]; !frameExists {
					stats.uniqueRtpTimestamps++
				}
				stats.rtpTimestamps[stats.packets[i].rtpTimestamp]++
			}

			elapsedUS := stats.packets[StatsBufferSize-1].clientReadTime - stats.packets[0].clientReadTime
			if elapsedUS <= 0 {
				elapsedUS = 1
			}
			bufferFPS := float32(stats.uniqueRtpTimestamps) / (float32(elapsedUS) / float32(1_000_000))
			stats.latestSmoothFPS = bufferFPS

			for i := 0; i < StatsBufferSize; i++ {
				stats.packets[i].bufferFPS = bufferFPS
			}

			stats.pos = 0
		}
		return
	}

	stats.layers[evictedPacket.spatialID][evictedPacket.temporalID]--
	stats.layers[spatialID][temporalID]++

	stats.rtpTimestamps[evictedPacket.rtpTimestamp]--
	if stats.rtpTimestamps[evictedPacket.rtpTimestamp] == 0 {
		delete(stats.rtpTimestamps, evictedPacket.rtpTimestamp)
		stats.uniqueRtpTimestamps--
	}

	_, frameExists := stats.rtpTimestamps[stats.packets[stats.pos].rtpTimestamp]
	stats.rtpTimestamps[stats.packets[stats.pos].rtpTimestamp]++

	if !frameExists {
		stats.uniqueRtpTimestamps++
	}

	elapsedUS := stats.packets[stats.pos].clientReadTime - stats.packets[(stats.pos+1)%StatsBufferSize].clientReadTime
	if elapsedUS <= 0 {
		elapsedUS = 1
	}
	bufferFPS := float32(stats.uniqueRtpTimestamps) / (float32(elapsedUS) / float32(1_000_000))
	stats.packets[stats.pos].bufferFPS = bufferFPS

	if !frameExists {
		stats.latestSmoothFPS = fpsEWMAAlpha*stats.latestSmoothFPS + (1.0-fpsEWMAAlpha)*bufferFPS
	}

	stats.pos = (stats.pos + 1) % StatsBufferSize
}

// TakeSample copies the histogram and current FPS into sample (or copies zero's if sample is too small)
func (stats *FrameStatistics) TakeSample(sample *VideoQualitySample) {
	if stats.len < StatsBufferSize {
		*sample = VideoQualitySample{}
		return
	}

	for sid := 0; sid < nSpatialLayers; sid++ {
		for tid := 0; tid < nTemporalLayers; tid++ {
			sample.Layers[sid][tid] = stats.layers[sid][tid]
		}
	}

	sample.SmoothFPS = stats.latestSmoothFPS
}

// String formats the video quality sample for debug purposes
func (sample *VideoQualitySample) String() string {
	sb := strings.Builder{}
	fmt.Fprintf(&sb, "vp9.VideoQualitySample[smooth_fps=%0.4f;SID-TID:count=", sample.SmoothFPS)
	for sid := nSpatialLayers - 1; sid >= 0; sid-- {
		for tid := nTemporalLayers - 1; tid >= 0; tid-- {
			if sample.Layers[sid][tid] > 0 {
				fmt.Fprintf(&sb, "%d-%d:%d,", sid, tid, sample.Layers[sid][tid])
			}
		}
	}
	sb.WriteRune(']')
	return sb.String()
}
