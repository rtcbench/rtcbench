package vp9_stats

import (
	"encoding/json"
	"fmt"
	"math"

	"call.zip/internal/vp9"
)

const (
	maxLayerSpatialID  = 2
	maxLayerTemporalID = 2

	nSpatialLayers  = maxLayerSpatialID + 1
	nTemporalLayers = maxLayerTemporalID + 1

	StatsBufferSize = 768 // TODO: configurable buffer size (StatsBufferSize)

	fpsEWMAAlpha     = float32(0.9)
	bitrateEWMAAlpha = fpsEWMAAlpha
)

type FrameStatistics struct {
	// sampleSequenceNo this sequence number counts the number of calls to TakeSample which writes it to each sample
	sampleSequenceNo uint64

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

	// latestSmoothBPS latest computed smoothed bits per second value
	latestSmoothBitrate float32

	// skipInvalidPackets count of packets skipped from stats due to invalid data
	skipInvalidPackets int64

	// sumBytes total byte sum of all rtp payloads in the buffer
	sumBytes int64

	// initialized indicates that the buffer has been initialized
	initialized bool

	// sample contains measurements that reset on successful publish
	sample SampleData
}

// SampleData per-sample data which is reset on EndSample()
type SampleData struct {
	TotalBytes                int64 `json:"total_bytes"`
	FirstPacketClientReadTime int64 `json:"first_pkt_cl_read_t"`
	LastPacketClientReadTime  int64 `json:"last_pkt_cl_read_t"`
}

type frameInfo struct {
	// clientReadTime packet received-at time (epoch utc microseconds)
	clientReadTime int64

	// rtpTimestamp remote timestamp (all packets of the same frame have the same timestamp)
	rtpTimestamp uint32

	// nBytes is byte count of the packet used to calculate bitrate (configured by caller of AcceptPacket)
	nBytes int

	// bufferFPS FPS computed using start and end time of packets in buffer
	bufferFPS float32

	// bufferBitrate bits per second computed using start and end time of packets in buffer
	bufferBitrate float32

	// spatialID video resolution (see PayloadDescriptor.SID)
	spatialID uint8

	// temporalID video frame rate (see PayloadDescriptor.TID)
	temporalID uint8
}

type VideoQualitySample struct {
	// SequenceNo monotonic sequence number
	SequenceNo uint64 `json:"seq"`

	// Nickname participant nickname
	Nickname string `json:"nickname"`

	// SmoothFPS latest computed smoothed frame per second value
	SmoothFPS float32 `json:"sm_fps"`

	// BufferFPS latest computed bufferFPS (non-smoothed FPS)
	BufferFPS float32 `json:"buf_fps"`

	// SmoothBitrate latest computed smoothed bits per second value
	SmoothBitrate float32 `json:"sm_br"`

	// BufferBitrate latest computed bufferBitrate (non-smoothed bitrate)
	BufferBitrate float32 `json:"buf_br"`

	// EstimatedFPS best frame per second value we offer
	EstimatedFPS int `json:"est_fps"`

	// SVC scalable video coding specific measurements
	SVC SVCData `json:"svc"`

	// Sample contains the per-sample measurements
	Sample SampleData `json:"sample"`
}

type SVCData struct {
	// Layers histogram of SID x TID counts (counted by packet)
	Layers [nSpatialLayers][nTemporalLayers]int `json:"layers"`
}

// AcceptPacket updates the stat tracker with information about the newly arrived VP9 RTP packet
func (stats *FrameStatistics) AcceptPacket(clientReadTime int64, rtpTimestamp uint32, nBytes int, payloadDesc *vp9.PayloadDescriptor) {
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
		nBytes:         nBytes,
		bufferFPS:      float32(0),
		bufferBitrate:  float32(0),
		spatialID:      spatialID,
		temporalID:     temporalID,
	}

	if stats.len < StatsBufferSize {
		stats.bufferGrowthPhase()
		return
	}

	stats.layers[evictedPacket.spatialID][evictedPacket.temporalID]--
	stats.layers[spatialID][temporalID]++

	stats.rtpTimestamps[evictedPacket.rtpTimestamp]--
	if stats.rtpTimestamps[evictedPacket.rtpTimestamp] == 0 {
		delete(stats.rtpTimestamps, evictedPacket.rtpTimestamp)
		stats.uniqueRtpTimestamps--
	}

	stats.sumBytes -= int64(evictedPacket.nBytes)
	stats.sumBytes += int64(nBytes)

	_, frameExists := stats.rtpTimestamps[stats.packets[stats.pos].rtpTimestamp]
	stats.rtpTimestamps[stats.packets[stats.pos].rtpTimestamp]++

	if !frameExists {
		stats.uniqueRtpTimestamps++
	}

	elapsedUS := stats.packets[stats.pos].clientReadTime - stats.packets[(stats.pos+1)%StatsBufferSize].clientReadTime
	if elapsedUS <= 0 {
		elapsedUS = 1
	}

	// TODO: only latest bufferFPS and bufferBitrate is needed, uses extra memory in frameInfo
	bufferFPS := float32(stats.uniqueRtpTimestamps) / (float32(elapsedUS) / float32(1_000_000))
	stats.packets[stats.pos].bufferFPS = bufferFPS
	bufferBitrate := float32(stats.sumBytes*8) / (float32(elapsedUS) / float32(1_000_000))
	stats.packets[stats.pos].bufferBitrate = bufferBitrate

	if !frameExists {
		stats.latestSmoothFPS = fpsEWMAAlpha*stats.latestSmoothFPS + (1.0-fpsEWMAAlpha)*bufferFPS
	}

	stats.latestSmoothBitrate = bitrateEWMAAlpha*stats.latestSmoothBitrate + (1.0-bitrateEWMAAlpha)*bufferBitrate

	stats.pos = (stats.pos + 1) % StatsBufferSize

	// update sample data
	stats.sample.TotalBytes += int64(nBytes)
	if stats.sample.FirstPacketClientReadTime == 0 {
		stats.sample.FirstPacketClientReadTime = clientReadTime
	}
	stats.sample.LastPacketClientReadTime = clientReadTime
}

func (stats *FrameStatistics) bufferGrowthPhase() {
	stats.pos++
	stats.len++
	if stats.len == StatsBufferSize { // stats initialization
		stats.initBufferStats()
	}
}

func (stats *FrameStatistics) initBufferStats() {
	stats.rtpTimestamps = make(map[uint32]int) // TODO reuse map

	for i := 0; i < StatsBufferSize; i++ {
		stats.layers[stats.packets[i].spatialID][stats.packets[i].temporalID]++
		if _, frameExists := stats.rtpTimestamps[stats.packets[i].rtpTimestamp]; !frameExists {
			stats.uniqueRtpTimestamps++
		}
		stats.rtpTimestamps[stats.packets[i].rtpTimestamp]++
		stats.sumBytes += int64(stats.packets[i].nBytes)
	}

	elapsedUS := stats.packets[StatsBufferSize-1].clientReadTime - stats.packets[0].clientReadTime
	if elapsedUS <= 0 {
		elapsedUS = 1
	}

	bufferFPS := float32(stats.uniqueRtpTimestamps) / (float32(elapsedUS) / float32(1_000_000))
	stats.latestSmoothFPS = bufferFPS

	bufferBitrate := float32(stats.sumBytes*8) / (float32(elapsedUS) / float32(1_000_000))
	stats.latestSmoothBitrate = bufferBitrate

	for i := 0; i < StatsBufferSize; i++ {
		stats.packets[i].bufferFPS = bufferFPS
		stats.packets[i].bufferBitrate = bufferBitrate
	}

	stats.pos = 0
	stats.initialized = true
}

// TakeSample copies the histogram and current FPS into sample (or copies zero's if sample is too small)
func (stats *FrameStatistics) TakeSample(sample *VideoQualitySample) {
	if stats.len < StatsBufferSize {
		*sample = VideoQualitySample{
			SequenceNo: stats.sampleSequenceNo,
		}
		return
	}

	sample.SequenceNo = stats.sampleSequenceNo
	sample.Sample = stats.sample

	for sid := 0; sid < nSpatialLayers; sid++ {
		for tid := 0; tid < nTemporalLayers; tid++ {
			sample.SVC.Layers[sid][tid] = stats.layers[sid][tid]
		}
	}

	// use stats.pos-1 because stats.pos points to the next eviction (eldest member)
	lastPos := stats.pos - 1
	if lastPos < 0 {
		lastPos = StatsBufferSize - 1
	}
	sample.SmoothFPS = stats.latestSmoothFPS
	sample.BufferFPS = stats.packets[lastPos].bufferFPS
	sample.SmoothBitrate = stats.latestSmoothBitrate
	sample.BufferBitrate = stats.packets[lastPos].bufferBitrate

	sample.EstimatedFPS = int(math.Ceil(float64(sample.SmoothFPS))) - 2 /* we overcount the first and last frames */
}

// EndSample must be called if your data in TakeSample was successfully published,
// otherwise don't call this and let the sample grow automatically
func (stats *FrameStatistics) EndSample() {
	stats.sample = SampleData{}
	stats.sampleSequenceNo++
}

func (stats *FrameStatistics) Initialized() bool {
	return stats.initialized
}

// String formats the video quality sample for debug purposes
func (sample *VideoQualitySample) String() string {
	b, _ := json.Marshal(sample)
	return fmt.Sprintf("vp9.VideoQualitySample[%s]", string(b))
}
