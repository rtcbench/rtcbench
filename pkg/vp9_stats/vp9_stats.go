package vp9_stats

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/rtcbench/rtcbench/pkg/vp9"
)

const (
	maxLayerSpatialID  = 2
	maxLayerTemporalID = 2

	nSpatialLayers  = maxLayerSpatialID + 1
	nTemporalLayers = maxLayerTemporalID + 1

	DefaultStatsBufferSize = 768

	fpsEWMAAlpha     = float32(0.9)
	bitrateEWMAAlpha = fpsEWMAAlpha

	// frameJitterGain is 1/16, matching RFC 3550 jitter EWMA gain but applied per-frame
	frameJitterGain = 1.0 / 16.0
)

// pendingFrame accumulates packets for the current in-progress frame
type pendingFrame struct {
	rtpTimestamp uint32
	firstArrival int64
	lastArrival  int64
	firstSeqNo   uint16
	nextSeqNo    uint16 // expected next sequence number (firstSeqNo + packetCount)
	packetCount  int
	totalBytes   int
	complete     bool // false if any sequence gap detected
}

type FrameStatistics struct {
	// onFrameLost is called when a frame is finalized as incomplete.
	// The argument is the current time in UnixNano. May be nil.
	onFrameLost func(nowNano int64)

	// bufferSize ring buffer capacity
	bufferSize int

	// sampleSequenceNo this sequence number counts the number of calls to TakeSample which writes it to each sample
	sampleSequenceNo uint64

	// pos current position in the buffer
	pos int

	// len number of packets in the buffer
	len int

	// packets buffer of parsed frameInfo per VP9 payload
	packets []frameInfo

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

	// --- frame tracking ---

	// currentFrame accumulates packets for the in-progress frame (nil before first packet)
	currentFrame *pendingFrame

	// prevFrameFirstArrival first packet arrival time of the previously finalized frame
	prevFrameFirstArrival int64

	// prevFrameRTPTimestamp RTP timestamp of the previously finalized frame
	prevFrameRTPTimestamp uint32

	// hasPrevFrame indicates that prevFrameFirstArrival is valid
	hasPrevFrame bool

	// frameComplete tracks whether each finalized frame (by RTP timestamp) was complete
	frameComplete map[uint32]bool

	// uniqueCompleteFrames count of complete frames currently in the ring buffer
	uniqueCompleteFrames int

	// latestDecoderBufferFPS most recent decoder buffer FPS (excludes incomplete frames)
	latestDecoderBufferFPS float32

	// latestSmoothDecoderFPS EWMA-smoothed decoder FPS
	latestSmoothDecoderFPS float32

	// frameJitterUS RFC 3550 style EWMA frame interarrival jitter in microseconds
	frameJitterUS float64

	// packet-level RFC 3550 interarrival jitter (EWMA, microseconds)
	prevPktArrival       int64
	prevPktRTPTimestamp  uint32
	packetJitterUS       float64

	// framesComplete monotonic count of finalized complete frames
	framesComplete int64

	// framesLost monotonic count of finalized incomplete frames
	framesLost int64
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

	// seqNo RTP sequence number
	seqNo uint16

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

	// DecoderSmoothFPS EWMA-smoothed FPS counting only decodable (complete) frames
	DecoderSmoothFPS float32 `json:"dec_sm_fps"`

	// DecoderBufferFPS buffer-window FPS counting only decodable (complete) frames
	DecoderBufferFPS float32 `json:"dec_buf_fps"`

	// EstimatedDecoderFPS best decodable frame per second value we offer
	EstimatedDecoderFPS int `json:"est_dec_fps"`

	// FrameJitterUS EWMA frame interarrival jitter in microseconds
	FrameJitterUS float64 `json:"frame_jitter_us"`

	// PacketJitterUS RFC 3550 packet-level interarrival jitter in microseconds
	PacketJitterUS float64 `json:"packet_jitter_us"`

	// FramesComplete monotonic count of complete frames
	FramesComplete int64 `json:"frames_complete"`

	// FramesLost monotonic count of incomplete (undecodable) frames
	FramesLost int64 `json:"frames_lost"`

	// SVC scalable video coding specific measurements
	SVC SVCData `json:"svc"`

	// RTCP feedback stats (populated when RTCPTracker is present)
	RTCP RTCPData `json:"rtcp,omitzero"`

	// Sample contains the per-sample measurements
	Sample SampleData `json:"sample"`
}

type SVCData struct {
	// Layers histogram of SID x TID counts (counted by packet)
	Layers [nSpatialLayers][nTemporalLayers]int `json:"layers"`

	// MaxRecvSID highest spatial layer observed in the current sample window
	MaxRecvSID uint8 `json:"max_recv_sid"`

	// MaxRecvTID highest temporal layer observed in the current sample window
	MaxRecvTID uint8 `json:"max_recv_tid"`
}

// NewFrameStatistics creates a FrameStatistics with the given ring buffer capacity.
func NewFrameStatistics(bufferSize int) *FrameStatistics {
	return &FrameStatistics{
		bufferSize:    bufferSize,
		packets:       make([]frameInfo, bufferSize),
		frameComplete: make(map[uint32]bool),
	}
}

// SetOnFrameLost registers a callback invoked when a frame is finalized as
// incomplete. The argument is the current time in UnixNano.
func (stats *FrameStatistics) SetOnFrameLost(fn func(nowNano int64)) {
	stats.onFrameLost = fn
}

// AcceptPacket updates the stat tracker with information about the newly arrived VP9 RTP packet.
// seqNo is the RTP sequence number used for frame completeness detection.
// Out-of-order packets (sequence gap within a frame) mark the frame as incomplete.
func (stats *FrameStatistics) AcceptPacket(clientReadTime int64, seqNo uint16, rtpTimestamp uint32, nBytes int, payloadDesc *vp9.PayloadDescriptor) {
	spatialID := payloadDesc.SID
	temporalID := payloadDesc.TID

	if spatialID > maxLayerSpatialID || temporalID > maxLayerTemporalID {
		stats.skipInvalidPackets++
		return
	}

	// RFC 3550 packet-level interarrival jitter.
	// Only update for in-order packets (non-decreasing RTP timestamp).
	// Out-of-order packets — including NACK retransmissions that arrive with
	// stale RTP timestamps — would produce large negative dS values and inflate
	// the jitter EWMA by hundreds of ms.  Skipping them entirely (both the
	// EWMA update and the prevPkt state) keeps the measurement anchored to the
	// last in-order packet so the next in-order packet sees a correct baseline.
	if stats.prevPktArrival == 0 {
		// First packet: initialise state, skip jitter calculation.
		stats.prevPktArrival = clientReadTime
		stats.prevPktRTPTimestamp = rtpTimestamp
	} else if int32(rtpTimestamp-stats.prevPktRTPTimestamp) >= 0 {
		dR := float64(clientReadTime - stats.prevPktArrival)                                   // µs
		dS := float64(int32(rtpTimestamp-stats.prevPktRTPTimestamp)) * 1_000_000.0 / 90_000.0 // RTP ticks -> µs
		d := dR - dS
		if d < 0 {
			d = -d
		}
		stats.packetJitterUS += (d - stats.packetJitterUS) / 16.0
		stats.prevPktArrival = clientReadTime
		stats.prevPktRTPTimestamp = rtpTimestamp
	}
	// else: out-of-order packet — leave prevPkt unchanged.

	// --- frame boundary tracking (before ring buffer logic) ---
	stats.trackFrame(clientReadTime, seqNo, rtpTimestamp, nBytes)

	// --- ring buffer logic ---
	evictedPacket := stats.packets[stats.pos]

	stats.packets[stats.pos] = frameInfo{
		clientReadTime: clientReadTime,
		rtpTimestamp:   rtpTimestamp,
		seqNo:          seqNo,
		nBytes:         nBytes,
		bufferFPS:      float32(0),
		bufferBitrate:  float32(0),
		spatialID:      spatialID,
		temporalID:     temporalID,
	}

	if stats.len < stats.bufferSize {
		stats.bufferGrowthPhase()
		return
	}

	stats.layers[evictedPacket.spatialID][evictedPacket.temporalID]--
	stats.layers[spatialID][temporalID]++

	stats.rtpTimestamps[evictedPacket.rtpTimestamp]--
	if stats.rtpTimestamps[evictedPacket.rtpTimestamp] == 0 {
		delete(stats.rtpTimestamps, evictedPacket.rtpTimestamp)
		stats.uniqueRtpTimestamps--

		// evict frame completeness and update decoder frame count
		if complete, ok := stats.frameComplete[evictedPacket.rtpTimestamp]; ok {
			if complete {
				stats.uniqueCompleteFrames--
			}
			delete(stats.frameComplete, evictedPacket.rtpTimestamp)
		}
	}

	stats.sumBytes -= int64(evictedPacket.nBytes)
	stats.sumBytes += int64(nBytes)

	_, frameExists := stats.rtpTimestamps[stats.packets[stats.pos].rtpTimestamp]
	stats.rtpTimestamps[stats.packets[stats.pos].rtpTimestamp]++

	if !frameExists {
		stats.uniqueRtpTimestamps++

		// if frame was already finalized, count it for decoder FPS
		if complete, ok := stats.frameComplete[rtpTimestamp]; ok && complete {
			stats.uniqueCompleteFrames++
		}
	}

	elapsedUS := stats.packets[stats.pos].clientReadTime - stats.packets[(stats.pos+1)%stats.bufferSize].clientReadTime
	if elapsedUS <= 0 {
		elapsedUS = 1
	}

	// TODO: only latest bufferFPS and bufferBitrate is needed, uses extra memory in frameInfo
	bufferFPS := float32(stats.uniqueRtpTimestamps) / (float32(elapsedUS) / float32(1_000_000))
	stats.packets[stats.pos].bufferFPS = bufferFPS
	bufferBitrate := float32(stats.sumBytes*8) / (float32(elapsedUS) / float32(1_000_000))
	stats.packets[stats.pos].bufferBitrate = bufferBitrate

	decoderBufferFPS := float32(stats.uniqueCompleteFrames) / (float32(elapsedUS) / float32(1_000_000))
	stats.latestDecoderBufferFPS = decoderBufferFPS

	if !frameExists {
		stats.latestSmoothFPS = fpsEWMAAlpha*stats.latestSmoothFPS + (1.0-fpsEWMAAlpha)*bufferFPS
		stats.latestSmoothDecoderFPS = fpsEWMAAlpha*stats.latestSmoothDecoderFPS + (1.0-fpsEWMAAlpha)*decoderBufferFPS
	}

	stats.latestSmoothBitrate = bitrateEWMAAlpha*stats.latestSmoothBitrate + (1.0-bitrateEWMAAlpha)*bufferBitrate

	stats.pos = (stats.pos + 1) % stats.bufferSize

	// update sample data
	stats.sample.TotalBytes += int64(nBytes)
	if stats.sample.FirstPacketClientReadTime == 0 {
		stats.sample.FirstPacketClientReadTime = clientReadTime
	}
	stats.sample.LastPacketClientReadTime = clientReadTime
}

// trackFrame handles frame boundary detection and finalization.
// Must be called before ring buffer insertion so that frameComplete
// is populated before the new timestamp enters the buffer.
func (stats *FrameStatistics) trackFrame(clientReadTime int64, seqNo uint16, rtpTimestamp uint32, nBytes int) {
	if stats.currentFrame == nil {
		stats.currentFrame = &pendingFrame{
			rtpTimestamp: rtpTimestamp,
			firstArrival: clientReadTime,
			lastArrival:  clientReadTime,
			firstSeqNo:   seqNo,
			nextSeqNo:    seqNo + 1,
			packetCount:  1,
			totalBytes:   nBytes,
			complete:     true,
		}
		return
	}

	if rtpTimestamp != stats.currentFrame.rtpTimestamp {
		// new frame — finalize previous
		stats.finalizeFrame(stats.currentFrame)

		stats.currentFrame = &pendingFrame{
			rtpTimestamp: rtpTimestamp,
			firstArrival: clientReadTime,
			lastArrival:  clientReadTime,
			firstSeqNo:   seqNo,
			nextSeqNo:    seqNo + 1,
			packetCount:  1,
			totalBytes:   nBytes,
			complete:     true,
		}
		return
	}

	// same frame — accumulate
	if seqNo != stats.currentFrame.nextSeqNo {
		stats.currentFrame.complete = false
	}
	stats.currentFrame.lastArrival = clientReadTime
	stats.currentFrame.nextSeqNo = seqNo + 1
	stats.currentFrame.packetCount++
	stats.currentFrame.totalBytes += nBytes
}

// finalizeFrame records completeness and computes frame-level jitter.
func (stats *FrameStatistics) finalizeFrame(f *pendingFrame) {
	// record completeness
	stats.frameComplete[f.rtpTimestamp] = f.complete

	if f.complete {
		stats.framesComplete++
	} else {
		stats.framesLost++
		if stats.onFrameLost != nil {
			stats.onFrameLost(f.lastArrival * 1000) // convert µs → ns
		}
	}

	// if buffer is initialized and this frame is in the buffer, update decoder count
	if stats.initialized {
		if _, inBuffer := stats.rtpTimestamps[f.rtpTimestamp]; inBuffer && f.complete {
			stats.uniqueCompleteFrames++
		}
	}

	// frame jitter — RFC 3550 style: D = (Rj-Ri) - (Sj-Si)
	// Uses RTP timestamps to compensate for expected timing changes (e.g. layer switches).
	if stats.hasPrevFrame {
		dR := float64(f.firstArrival - stats.prevFrameFirstArrival)                                     // µs
		dS := float64(int32(f.rtpTimestamp-stats.prevFrameRTPTimestamp)) * 1_000_000.0 / 90_000.0        // RTP ticks -> µs
		d := dR - dS
		if d < 0 {
			d = -d
		}
		stats.frameJitterUS += (d - stats.frameJitterUS) * frameJitterGain
	}

	stats.prevFrameFirstArrival = f.firstArrival
	stats.prevFrameRTPTimestamp = f.rtpTimestamp
	stats.hasPrevFrame = true
}

func (stats *FrameStatistics) bufferGrowthPhase() {
	stats.pos++
	stats.len++
	if stats.len == stats.bufferSize { // stats initialization
		stats.initBufferStats()
	}
}

func (stats *FrameStatistics) initBufferStats() {
	stats.rtpTimestamps = make(map[uint32]int) // TODO reuse map

	for i := 0; i < stats.bufferSize; i++ {
		stats.layers[stats.packets[i].spatialID][stats.packets[i].temporalID]++
		if _, frameExists := stats.rtpTimestamps[stats.packets[i].rtpTimestamp]; !frameExists {
			stats.uniqueRtpTimestamps++
		}
		stats.rtpTimestamps[stats.packets[i].rtpTimestamp]++
		stats.sumBytes += int64(stats.packets[i].nBytes)
	}

	// count complete frames in the buffer
	stats.uniqueCompleteFrames = 0
	for ts := range stats.rtpTimestamps {
		if complete, ok := stats.frameComplete[ts]; ok && complete {
			stats.uniqueCompleteFrames++
		}
	}

	elapsedUS := stats.packets[stats.bufferSize-1].clientReadTime - stats.packets[0].clientReadTime
	if elapsedUS <= 0 {
		elapsedUS = 1
	}

	bufferFPS := float32(stats.uniqueRtpTimestamps) / (float32(elapsedUS) / float32(1_000_000))
	stats.latestSmoothFPS = bufferFPS

	bufferBitrate := float32(stats.sumBytes*8) / (float32(elapsedUS) / float32(1_000_000))
	stats.latestSmoothBitrate = bufferBitrate

	decoderBufferFPS := float32(stats.uniqueCompleteFrames) / (float32(elapsedUS) / float32(1_000_000))
	stats.latestDecoderBufferFPS = decoderBufferFPS
	stats.latestSmoothDecoderFPS = decoderBufferFPS

	for i := 0; i < stats.bufferSize; i++ {
		stats.packets[i].bufferFPS = bufferFPS
		stats.packets[i].bufferBitrate = bufferBitrate
	}

	stats.pos = 0
	stats.initialized = true
}

// TakeSample copies the histogram and current FPS into sample (or copies zero's if sample is too small)
func (stats *FrameStatistics) TakeSample(sample *VideoQualitySample) {
	if stats.len < stats.bufferSize {
		*sample = VideoQualitySample{
			SequenceNo: stats.sampleSequenceNo,
		}
		return
	}

	sample.SequenceNo = stats.sampleSequenceNo
	sample.Sample = stats.sample

	var maxSID, maxTID uint8
	for sid := range nSpatialLayers {
		for tid := range nTemporalLayers {
			sample.SVC.Layers[sid][tid] = stats.layers[sid][tid]
			if stats.layers[sid][tid] > 0 {
				if uint8(sid) > maxSID {
					maxSID = uint8(sid)
				}
				if uint8(tid) > maxTID {
					maxTID = uint8(tid)
				}
			}
		}
	}
	sample.SVC.MaxRecvSID = maxSID
	sample.SVC.MaxRecvTID = maxTID

	// use stats.pos-1 because stats.pos points to the next eviction (eldest member)
	lastPos := stats.pos - 1
	if lastPos < 0 {
		lastPos = stats.bufferSize - 1
	}
	sample.SmoothFPS = stats.latestSmoothFPS
	sample.BufferFPS = stats.packets[lastPos].bufferFPS
	sample.SmoothBitrate = stats.latestSmoothBitrate
	sample.BufferBitrate = stats.packets[lastPos].bufferBitrate

	sample.EstimatedFPS = int(math.Ceil(float64(sample.SmoothFPS))) - 2 /* we overcount the first and last frames */

	// decoder FPS (complete frames only)
	sample.DecoderSmoothFPS = stats.latestSmoothDecoderFPS
	sample.DecoderBufferFPS = stats.latestDecoderBufferFPS
	sample.EstimatedDecoderFPS = int(math.Ceil(float64(sample.DecoderSmoothFPS))) - 2

	// jitter
	sample.FrameJitterUS = stats.frameJitterUS
	sample.PacketJitterUS = stats.packetJitterUS

	// frame counts
	sample.FramesComplete = stats.framesComplete
	sample.FramesLost = stats.framesLost
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

// Mbps returns the smooth bitrate formatted as megabits per second, e.g. "1.23 Mbps"
func (sample *VideoQualitySample) Mbps() string {
	return fmt.Sprintf("%0.2f Mbps", sample.SmoothBitrate/1_000_000)
}
