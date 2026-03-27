package vp9_stats

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"testing"
	"time"
	"unsafe"

	"call.zip/pkg/vp9"
)

const (
	// bitrate1080p25fps is the bitrate of a 1080p video at 25 FPS and is divisible by paySize
	bitrate1080p25fps = 3_494_400.0

	// paySize RTP payload size in bytes
	paySize = 1120
)

type testPacket struct {
	clientReadTime int64
	seqNo          uint16
	rtpTimestamp   uint32
	nBytes         int
	payloadDesc    vp9.PayloadDescriptor
}

type testBigPacket struct {
	testPacket
	payload [paySize]byte
}

func withRandomPayloads(testPackets []testPacket) []testBigPacket {
	bigPackets := make([]testBigPacket, len(testPackets))
	for i := range testPackets {
		bigPackets[i] = testBigPacket{
			testPacket: testPackets[i],
			payload:    [paySize]byte{},
		}
		for j := 0; j < testPackets[i].nBytes; j++ {
			bigPackets[i].payload[j] = byte(rand.Intn(256))
		}
	}
	return bigPackets
}

func createTestPackets1080p25fps(seconds int) []testPacket {
	const (
		// tps ticks per second (written to RTP timestamp)
		tps = 90_000.0

		// fps frames per second (fps should divide tps, bps, and size for these tests)
		fps = 25.0

		// bps bits per second
		bps = bitrate1080p25fps

		// size bytes per packet
		size = float64(paySize)

		// pps packets per second
		pps = (bps / 8.0) / size

		// ppf packets per frame
		ppf = pps / fps

		// tpf ticks per frame
		tpf = tps / fps

		// tpp ticks per packet
		tpp = tpf / ppf

		// spp seconds per packet
		spp  = 1.0 / pps
		_spp = tpp / tps
	)

	if spp != _spp {
		panic("math issue, check ints vs floats")
	}

	nPackets := int(pps * float64(seconds))
	pkts := make([]testPacket, nPackets)

	// rtpTimestamp is randomized and forces rollover
	rtpTimestamp := uint32(math.MaxUint32) -
		uint32(nPackets/2) +
		uint32(rand.Intn(nPackets/4))

	frameRTPTimestamp := rtpTimestamp

	// seqNo starts at a random offset and wraps naturally via uint16
	seqNo := uint16(rand.Intn(math.MaxUint16))

	// clientReadTime ends just before current time, so all tests will have different times
	clientReadTime := time.Now().Add(time.Duration(-seconds)*time.Second - 1*time.Second)

	for i := range nPackets {
		ppf_ := ppf
		if i%int(ppf_) == 0 {
			frameRTPTimestamp = rtpTimestamp
		}

		pkts[i] = testPacket{
			clientReadTime: clientReadTime.UnixMicro(),
			seqNo:          seqNo,
			rtpTimestamp:   frameRTPTimestamp,
			nBytes:         int(size),
			payloadDesc: vp9.PayloadDescriptor{
				SID: maxLayerSpatialID,
				TID: maxLayerTemporalID,
			},
		}
		seqNo++
		tpp_ := tpp
		rtpTimestamp += uint32(int64(tpp_))

		spp_ := spp
		readOffset := uint32(spp_ * 1_000_000)
		clientReadTime = clientReadTime.Add(time.Duration(readOffset) * time.Microsecond)
	}

	return pkts
}

func Test_createTestPackets1080p25fps_sanity(t *testing.T) {
	seconds := 3

	testVideo := createTestPackets1080p25fps(seconds)
	realVideo := withRandomPayloads(testVideo)

	if paySize%8 != 0 {
		t.Errorf("paySize %d should be divisible by 8 (alignment) for sizeof sanity check to work", paySize)
	}

	testVideoBytes := int(unsafe.Sizeof(testPacket{})) * len(testVideo)
	realVideoBytes := int(unsafe.Sizeof(testBigPacket{})) * len(realVideo)

	payloadSumA := 0
	for i := 0; i < len(realVideo); i++ {
		payloadSumA += realVideo[i].nBytes
	}
	payloadSumB := realVideoBytes - testVideoBytes
	if payloadSumA != payloadSumB {
		t.Errorf("payloadA(%d) != payloadB(%d): should be fixed size arrays", payloadSumA, payloadSumB)
	}
	payloadSum := payloadSumA
	expectedPayloadSum := seconds * (bitrate1080p25fps / 8)

	if payloadSum != expectedPayloadSum {
		log.Printf("testVideoBytes: %d, realVideoBytes: %d, payloadSum: %d",
			testVideoBytes,
			realVideoBytes,
			payloadSum)
		t.Errorf("payload sum incorrect: expected %d bytes, got %d bytes",
			expectedPayloadSum,
			payloadSum)
	}
}

func TestFrameStatistics_AcceptPacketAndTakeSample_SimpleVideos(t *testing.T) {
	// 2 seconds: set as short as possible, to see how quick we can initialize
	// 30 seconds, 5 minutes: longer video durations more real-world
	videoDurations := []int{2, 30, 5 * 60}

	for _, seconds := range videoDurations {
		stats := NewFrameStatistics(DefaultStatsBufferSize)
		var sample VideoQualitySample

		// simple "perfect" test packets
		testPackets := createTestPackets1080p25fps(seconds)

		nextSeq := uint64(0)

		checkSampleSequenceNumber := func() {
			if sample.SequenceNo != nextSeq {
				t.Fatalf("[seconds=%d] SequenceNo mismatch: got %d, want %d", seconds, sample.SequenceNo, nextSeq)
			}
			nextSeq++
		}

		checkUninitializedSample := func() {
			// All histogram buckets should be zero
			for sid := 0; sid < nSpatialLayers; sid++ {
				for tid := 0; tid < nTemporalLayers; tid++ {
					if sample.SVC.Layers[sid][tid] != 0 {
						t.Fatalf("[seconds=%d] expected zero layers during growth phase, got Layers[%d][%d]=%d",
							seconds, sid, tid, sample.SVC.Layers[sid][tid])
					}
				}
			}

			// All metrics should be zero during growth phase
			if sample.SmoothFPS != 0 {
				t.Fatalf("[seconds=%d] expected SmoothFPS=0 during growth phase, got %f",
					seconds, sample.SmoothFPS)
			}
			if sample.BufferFPS != 0 {
				t.Fatalf("[seconds=%d] expected BufferFPS=0 during growth phase, got %f",
					seconds, sample.BufferFPS)
			}
			if sample.SmoothBitrate != 0 {
				t.Fatalf("[seconds=%d] expected SmoothBitrate=0 during growth phase, got %f",
					seconds, sample.SmoothBitrate)
			}
			if sample.BufferBitrate != 0 {
				t.Fatalf("[seconds=%d] expected BufferBitrate=0 during growth phase, got %f",
					seconds, sample.BufferBitrate)
			}
		}

		wasInitialized := false

		checkInitializedSample := func() {
			wasInitialized = true

			if sample.SequenceNo < DefaultStatsBufferSize-1 {
				t.Fatalf("[seconds=%d] initialized seq no < DefaultStatsBufferSize", seconds)
			}

			if sample.EstimatedFPS != 25 {
				t.Fatalf("[seconds=%d;seq=%d] expected EstimatedFPS=25, got %v",
					seconds, sample.SequenceNo, sample.EstimatedFPS)
			}

			if sample.SVC.Layers[maxLayerSpatialID][maxLayerTemporalID] != DefaultStatsBufferSize {
				t.Fatalf("[seconds=%d;seq=%d] expected all SIDxTID's to be 2x2 got %d",
					seconds, sample.SequenceNo,
					sample.SVC.Layers[maxLayerSpatialID][maxLayerTemporalID])
			}
		}

		checkSample := func() {
			checkSampleSequenceNumber()
			if !stats.Initialized() {
				checkUninitializedSample()
				return
			}
			checkInitializedSample()
		}

		for _, pkt := range testPackets {
			stats.AcceptPacket(pkt.clientReadTime, pkt.seqNo, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
			stats.TakeSample(&sample)
			stats.EndSample()
			checkSample()
		}

		if !wasInitialized {
			t.Fatalf("[seconds=%d] did not initialize, use a longer video time", seconds)
		}
	}
}

func TestFrameStatistics_CustomBufferSize(t *testing.T) {
	bufferSizes := []int{128, 256, 512}

	for _, bufSize := range bufferSizes {
		stats := NewFrameStatistics(bufSize)
		var sample VideoQualitySample

		testPackets := createTestPackets1080p25fps(10)

		initialized := false
		var lastSample VideoQualitySample
		for _, pkt := range testPackets {
			stats.AcceptPacket(pkt.clientReadTime, pkt.seqNo, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
			stats.TakeSample(&sample)
			stats.EndSample()

			if stats.Initialized() && !initialized {
				initialized = true

				if sample.SVC.Layers[maxLayerSpatialID][maxLayerTemporalID] != bufSize {
					t.Fatalf("[bufSize=%d] expected layer count=%d, got %d",
						bufSize, bufSize, sample.SVC.Layers[maxLayerSpatialID][maxLayerTemporalID])
				}
			}
			lastSample = sample
		}

		if !initialized {
			t.Fatalf("[bufSize=%d] did not initialize", bufSize)
		}

		// After processing 10s of video, stats should be non-zero
		if lastSample.SmoothFPS <= 0 {
			t.Fatalf("[bufSize=%d] expected positive SmoothFPS, got %f", bufSize, lastSample.SmoothFPS)
		}
		if lastSample.SmoothBitrate <= 0 {
			t.Fatalf("[bufSize=%d] expected positive SmoothBitrate, got %f", bufSize, lastSample.SmoothBitrate)
		}
	}
}

func TestVideoQualitySample_String(t *testing.T) {
	sample := VideoQualitySample{
		SmoothFPS:     25,
		SmoothBitrate: 3500000,
		Nickname:      "test-viewer",
	}
	got := sample.String()

	prefix := "vp9.VideoQualitySample["
	if len(got) < len(prefix) || got[:len(prefix)] != prefix {
		t.Fatalf("String() = %q, want prefix %q", got, prefix)
	}

	// Check that the nickname is present in the output
	found := false
	for i := 0; i <= len(got)-len("test-viewer"); i++ {
		if got[i:i+len("test-viewer")] == "test-viewer" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("String() = %q, want it to contain %q", got, "test-viewer")
	}
}

func TestVideoQualitySample_Mbps(t *testing.T) {
	tests := []struct {
		smoothBitrate float32
		want          string
	}{
		{0, "0.00 Mbps"},
		{3_500_000, "3.50 Mbps"},
		{500_000, "0.50 Mbps"},
		{1_000_000, "1.00 Mbps"},
	}

	for _, tc := range tests {
		sample := VideoQualitySample{SmoothBitrate: tc.smoothBitrate}
		got := sample.Mbps()
		if got != tc.want {
			t.Fatalf("Mbps() with SmoothBitrate=%v = %q, want %q", tc.smoothBitrate, got, tc.want)
		}
	}
}

func TestFrameStatistics_EndSample_Resets(t *testing.T) {
	stats := NewFrameStatistics(DefaultStatsBufferSize)
	testPackets := createTestPackets1080p25fps(2)

	for _, pkt := range testPackets {
		stats.AcceptPacket(pkt.clientReadTime, pkt.seqNo, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
	}

	var sample VideoQualitySample
	stats.TakeSample(&sample)

	if sample.Sample.TotalBytes <= 0 {
		t.Fatalf("expected TotalBytes > 0 after feeding packets, got %d", sample.Sample.TotalBytes)
	}

	firstSeq := sample.SequenceNo
	stats.EndSample()

	// TakeSample again after EndSample — sample data should be reset
	stats.TakeSample(&sample)

	if sample.Sample.TotalBytes != 0 {
		t.Fatalf("expected TotalBytes == 0 after EndSample, got %d", sample.Sample.TotalBytes)
	}

	if sample.SequenceNo != firstSeq+1 {
		t.Fatalf("expected SequenceNo = %d after EndSample, got %d", firstSeq+1, sample.SequenceNo)
	}
}

func TestFrameStatistics_Initialized_Boundary(t *testing.T) {
	bufferSize := 64
	stats := NewFrameStatistics(bufferSize)

	baseTime := time.Now().UnixMicro()
	var rtpTS uint32 = 1000
	var seqNo uint16 = 0

	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	// Feed 63 packets (one less than bufferSize)
	for i := 0; i < bufferSize-1; i++ {
		stats.AcceptPacket(baseTime+int64(i)*1000, seqNo, rtpTS, 100, &pd)
		seqNo++
		rtpTS += 3600 // increment per packet
	}

	if stats.Initialized() {
		t.Fatalf("expected Initialized() == false after %d packets (bufferSize=%d)", bufferSize-1, bufferSize)
	}

	// Feed the 64th packet
	stats.AcceptPacket(baseTime+int64(bufferSize-1)*1000, seqNo, rtpTS, 100, &pd)

	if !stats.Initialized() {
		t.Fatalf("expected Initialized() == true after %d packets (bufferSize=%d)", bufferSize, bufferSize)
	}
}

func TestFrameStatistics_TakeSample_BeforeInitialized(t *testing.T) {
	stats := NewFrameStatistics(DefaultStatsBufferSize)

	baseTime := time.Now().UnixMicro()
	var rtpTS uint32 = 5000
	var seqNo uint16 = 0

	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	// Feed only 10 packets (well below the 768 buffer size)
	for i := 0; i < 10; i++ {
		stats.AcceptPacket(baseTime+int64(i)*1000, seqNo, rtpTS, 200, &pd)
		seqNo++
		rtpTS += 3600
	}

	if stats.Initialized() {
		t.Fatalf("expected Initialized() == false after 10 packets with bufferSize=%d", DefaultStatsBufferSize)
	}

	var sample VideoQualitySample
	stats.TakeSample(&sample)

	if sample.SmoothBitrate != 0 {
		t.Fatalf("expected SmoothBitrate == 0 before initialized, got %f", sample.SmoothBitrate)
	}
	if sample.SmoothFPS != 0 {
		t.Fatalf("expected SmoothFPS == 0 before initialized, got %f", sample.SmoothFPS)
	}
	if sample.BufferBitrate != 0 {
		t.Fatalf("expected BufferBitrate == 0 before initialized, got %f", sample.BufferBitrate)
	}
	if sample.BufferFPS != 0 {
		t.Fatalf("expected BufferFPS == 0 before initialized, got %f", sample.BufferFPS)
	}
}

// TestFrameStatistics_DecoderFPS_PerfectStream verifies that with no packet loss,
// DecoderSmoothFPS equals SmoothFPS and FramesLost is zero.
func TestFrameStatistics_DecoderFPS_PerfectStream(t *testing.T) {
	stats := NewFrameStatistics(DefaultStatsBufferSize)
	testPackets := createTestPackets1080p25fps(10)

	var sample VideoQualitySample
	for _, pkt := range testPackets {
		stats.AcceptPacket(pkt.clientReadTime, pkt.seqNo, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
	}

	stats.TakeSample(&sample)

	if sample.FramesLost != 0 {
		t.Fatalf("expected FramesLost=0 for perfect stream, got %d", sample.FramesLost)
	}

	if sample.FramesComplete <= 0 {
		t.Fatalf("expected FramesComplete > 0, got %d", sample.FramesComplete)
	}

	// With no loss, decoder FPS should match regular FPS
	fpsDelta := math.Abs(float64(sample.DecoderSmoothFPS - sample.SmoothFPS))
	if fpsDelta > 1.0 {
		t.Fatalf("expected DecoderSmoothFPS ≈ SmoothFPS for perfect stream, got decoder=%f smooth=%f delta=%f",
			sample.DecoderSmoothFPS, sample.SmoothFPS, fpsDelta)
	}

	if sample.EstimatedDecoderFPS != sample.EstimatedFPS {
		t.Fatalf("expected EstimatedDecoderFPS=%d == EstimatedFPS=%d for perfect stream",
			sample.EstimatedDecoderFPS, sample.EstimatedFPS)
	}
}

// TestFrameStatistics_DecoderFPS_WithLoss verifies that dropping a packet within a frame
// causes that frame to be marked as lost and reduces decoder FPS.
func TestFrameStatistics_DecoderFPS_WithLoss(t *testing.T) {
	stats := NewFrameStatistics(128) // smaller buffer for faster init
	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	baseTime := time.Now().UnixMicro()

	const (
		ppf         = 4     // packets per frame
		fps         = 25    // frames per second
		frameTimeUS = 40000 // 1/25 second in microseconds
		pktTimeUS   = frameTimeUS / ppf
		tpf         = 3600 // RTP ticks per frame
		nFrames     = 200  // enough to fill buffer and run steady state
	)

	var seqNo uint16 = 0
	var rtpTS uint32 = 1000

	lostFrames := 0
	completeFrames := 0

	for f := 0; f < nFrames; f++ {
		dropPacket := (f%10 == 5) // drop 1 packet in every 10th frame (at frame index 5, 15, 25, ...)

		for p := 0; p < ppf; p++ {
			pktTime := baseTime + int64(f)*frameTimeUS + int64(p)*pktTimeUS

			if dropPacket && p == 2 {
				// skip this packet — simulates loss
				seqNo++
				continue
			}

			stats.AcceptPacket(pktTime, seqNo, rtpTS, paySize, &pd)
			seqNo++
		}

		if dropPacket {
			lostFrames++
		} else {
			completeFrames++
		}

		rtpTS += tpf
	}

	var sample VideoQualitySample
	stats.TakeSample(&sample)

	if sample.FramesLost == 0 {
		t.Fatalf("expected FramesLost > 0, got 0")
	}

	if sample.FramesComplete == 0 {
		t.Fatalf("expected FramesComplete > 0, got 0")
	}

	// FramesLost should be approximately 10% of total frames
	totalFrames := sample.FramesComplete + sample.FramesLost
	lossRate := float64(sample.FramesLost) / float64(totalFrames)
	if lossRate < 0.05 || lossRate > 0.15 {
		t.Fatalf("expected ~10%% frame loss rate, got %.1f%% (complete=%d lost=%d)",
			lossRate*100, sample.FramesComplete, sample.FramesLost)
	}

	// Decoder FPS should be lower than regular FPS
	if sample.DecoderSmoothFPS >= sample.SmoothFPS {
		t.Fatalf("expected DecoderSmoothFPS(%f) < SmoothFPS(%f) when frames are lost",
			sample.DecoderSmoothFPS, sample.SmoothFPS)
	}
}

// TestFrameStatistics_FrameJitter_PerfectStream verifies that with perfectly spaced frames,
// frame jitter converges to near zero.
func TestFrameStatistics_FrameJitter_PerfectStream(t *testing.T) {
	stats := NewFrameStatistics(DefaultStatsBufferSize)
	testPackets := createTestPackets1080p25fps(30) // 30 seconds for EWMA to converge

	for _, pkt := range testPackets {
		stats.AcceptPacket(pkt.clientReadTime, pkt.seqNo, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
	}

	var sample VideoQualitySample
	stats.TakeSample(&sample)

	// With perfectly even spacing, frame jitter should converge near zero.
	// Allow tolerance for integer rounding of both µs arrival times and RTP ticks.
	if sample.FrameJitterUS > 200.0 {
		t.Fatalf("expected FrameJitterUS ≈ 0 for perfect stream, got %f", sample.FrameJitterUS)
	}
}

// TestFrameStatistics_FrameJitter_WithVariance verifies that frame jitter is nonzero
// when frame arrival times have variance.
func TestFrameStatistics_FrameJitter_WithVariance(t *testing.T) {
	stats := NewFrameStatistics(128)
	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	baseTime := time.Now().UnixMicro()

	const (
		ppf         = 4
		frameTimeUS = 40000 // 25fps
		pktTimeUS   = frameTimeUS / ppf
		tpf         = 3600
		nFrames     = 200
	)

	var seqNo uint16 = 0
	var rtpTS uint32 = 1000
	rng := rand.New(rand.NewSource(42))

	for f := 0; f < nFrames; f++ {
		// add random jitter: ±5ms to frame start time
		jitterUS := int64(rng.Intn(10000) - 5000)
		frameStart := baseTime + int64(f)*frameTimeUS + jitterUS

		for p := 0; p < ppf; p++ {
			pktTime := frameStart + int64(p)*pktTimeUS
			stats.AcceptPacket(pktTime, seqNo, rtpTS, paySize, &pd)
			seqNo++
		}
		rtpTS += tpf
	}

	var sample VideoQualitySample
	stats.TakeSample(&sample)

	// With ±5ms jitter, frame jitter should be significantly nonzero
	if sample.FrameJitterUS < 100.0 {
		t.Fatalf("expected FrameJitterUS > 100 with ±5ms variance, got %f", sample.FrameJitterUS)
	}

	// But shouldn't be insanely high — ±5ms jitter means max deviation ~10ms
	if sample.FrameJitterUS > 15000.0 {
		t.Fatalf("expected FrameJitterUS < 15000 with ±5ms variance, got %f", sample.FrameJitterUS)
	}
}

// TestFrameStatistics_FrameCompleteness_SequenceGap verifies that a gap in sequence numbers
// within a frame marks it as incomplete.
func TestFrameStatistics_FrameCompleteness_SequenceGap(t *testing.T) {
	stats := NewFrameStatistics(64)
	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	baseTime := time.Now().UnixMicro()

	// Frame 1: packets seq 0,1,2 (complete)
	var rtpTS uint32 = 1000
	stats.AcceptPacket(baseTime, 0, rtpTS, 100, &pd)
	stats.AcceptPacket(baseTime+100, 1, rtpTS, 100, &pd)
	stats.AcceptPacket(baseTime+200, 2, rtpTS, 100, &pd)

	// Frame 2: packets seq 3,5 (gap — missing seq 4)
	rtpTS = 4600
	stats.AcceptPacket(baseTime+40000, 3, rtpTS, 100, &pd)
	stats.AcceptPacket(baseTime+40100, 5, rtpTS, 100, &pd) // seq 5, expected 4

	// Frame 3: packets seq 6,7 (complete — triggers finalization of frame 2)
	rtpTS = 8200
	stats.AcceptPacket(baseTime+80000, 6, rtpTS, 100, &pd)
	stats.AcceptPacket(baseTime+80100, 7, rtpTS, 100, &pd)

	// Frame 4: trigger finalization of frame 3 by starting frame 4
	rtpTS = 11800
	stats.AcceptPacket(baseTime+120000, 8, rtpTS, 100, &pd)

	// At this point: frame 1 complete, frame 2 incomplete, frame 3 complete
	// (frame 4 is still in-progress, not finalized)
	if stats.framesComplete != 2 {
		t.Fatalf("expected framesComplete=2, got %d", stats.framesComplete)
	}
	if stats.framesLost != 1 {
		t.Fatalf("expected framesLost=1, got %d", stats.framesLost)
	}
}

// TestFrameStatistics_FrameJitter_IgnoresCompleteness verifies that frame jitter is computed
// for all frames regardless of whether they are complete.
func TestFrameStatistics_FrameJitter_IgnoresCompleteness(t *testing.T) {
	stats := NewFrameStatistics(128)
	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	baseTime := time.Now().UnixMicro()

	const (
		ppf         = 4
		frameTimeUS = int64(40000)
		pktTimeUS   = frameTimeUS / ppf
		tpf         = uint32(3600)
		nFrames     = 150 // enough to fill 128-packet buffer and converge EWMA
	)

	var seqNo uint16 = 0
	var rtpTS uint32 = 1000

	for f := 0; f < nFrames; f++ {
		// every 5th frame has a gap (incomplete)
		dropPacket := (f%5 == 3)

		for p := 0; p < ppf; p++ {
			pktTime := baseTime + int64(f)*frameTimeUS + int64(p)*pktTimeUS

			if dropPacket && p == 1 {
				seqNo++ // skip
				continue
			}

			stats.AcceptPacket(pktTime, seqNo, rtpTS, 100, &pd)
			seqNo++
		}
		rtpTS += tpf
	}

	// Jitter should still be computed (near zero since timing is perfect)
	if stats.frameJitterUS < 0 {
		t.Fatalf("expected non-negative frameJitterUS, got %f", stats.frameJitterUS)
	}

	// Even with packet loss, frames still arrive at regular intervals,
	// so jitter should be very low
	if stats.frameJitterUS > 200.0 {
		t.Fatalf("expected low frameJitterUS for evenly-spaced frames (even with loss), got %f",
			stats.frameJitterUS)
	}

	// Verify we actually had some lost frames
	if stats.framesLost == 0 {
		t.Fatalf("expected framesLost > 0 in this test")
	}
}

// TestFrameStatistics_DecoderFPS_SampleOutput verifies that TakeSample correctly
// populates the decoder FPS and frame count fields.
func TestFrameStatistics_DecoderFPS_SampleOutput(t *testing.T) {
	stats := NewFrameStatistics(128)
	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	baseTime := time.Now().UnixMicro()

	const (
		ppf         = 4
		frameTimeUS = int64(40000)
		pktTimeUS   = frameTimeUS / ppf
		tpf         = uint32(3600)
		nFrames     = 200
	)

	var seqNo uint16 = 0
	var rtpTS uint32 = 1000

	for f := 0; f < nFrames; f++ {
		for p := 0; p < ppf; p++ {
			pktTime := baseTime + int64(f)*frameTimeUS + int64(p)*pktTimeUS
			stats.AcceptPacket(pktTime, seqNo, rtpTS, paySize, &pd)
			seqNo++
		}
		rtpTS += tpf
	}

	var sample VideoQualitySample
	stats.TakeSample(&sample)

	// Verify all new fields are populated in the sample
	if sample.DecoderSmoothFPS <= 0 {
		t.Fatalf("expected DecoderSmoothFPS > 0, got %f", sample.DecoderSmoothFPS)
	}
	if sample.DecoderBufferFPS <= 0 {
		t.Fatalf("expected DecoderBufferFPS > 0, got %f", sample.DecoderBufferFPS)
	}
	if sample.FramesComplete <= 0 {
		t.Fatalf("expected FramesComplete > 0, got %d", sample.FramesComplete)
	}
	if sample.FramesLost != 0 {
		t.Fatalf("expected FramesLost=0, got %d", sample.FramesLost)
	}

	// For a perfect stream, EstimatedDecoderFPS should be close to 25
	if sample.EstimatedDecoderFPS < 20 || sample.EstimatedDecoderFPS > 30 {
		t.Fatalf("expected EstimatedDecoderFPS ≈ 25, got %d", sample.EstimatedDecoderFPS)
	}

	// frame jitter should be present in serialized output
	s := sample.String()
	if len(s) == 0 {
		t.Fatalf("expected non-empty String() output")
	}
	// Verify JSON contains new fields
	for _, field := range []string{"dec_sm_fps", "dec_buf_fps", "est_dec_fps", "frame_jitter_us", "frames_complete", "frames_lost"} {
		found := false
		for i := 0; i <= len(s)-len(field); i++ {
			if s[i:i+len(field)] == field {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected String() to contain %q, got %s", field, s)
		}
	}
}

// TestFrameStatistics_SeqNoWrapAround verifies that uint16 sequence number wraparound
// within a frame does not falsely mark the frame as incomplete.
func TestFrameStatistics_SeqNoWrapAround(t *testing.T) {
	stats := NewFrameStatistics(64)
	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	baseTime := time.Now().UnixMicro()

	// Frame spanning uint16 wraparound: seq 65534, 65535, 0, 1
	var rtpTS uint32 = 1000
	stats.AcceptPacket(baseTime, 65534, rtpTS, 100, &pd)
	stats.AcceptPacket(baseTime+100, 65535, rtpTS, 100, &pd)
	stats.AcceptPacket(baseTime+200, 0, rtpTS, 100, &pd) // wraps to 0
	stats.AcceptPacket(baseTime+300, 1, rtpTS, 100, &pd)

	// Start next frame to finalize the first
	rtpTS = 4600
	stats.AcceptPacket(baseTime+40000, 2, rtpTS, 100, &pd)

	// The first frame should be complete — uint16 addition handles wraparound naturally
	if stats.framesComplete != 1 {
		t.Fatalf("expected framesComplete=1 after seqNo wraparound, got %d", stats.framesComplete)
	}
	if stats.framesLost != 0 {
		t.Fatalf("expected framesLost=0 after seqNo wraparound, got %d", stats.framesLost)
	}
}

// TestPacketJitter_SkipsOutOfOrder verifies that out-of-order packets (e.g. NACK
// retransmissions with stale RTP timestamps) do not inflate packet_jitter_us.
// The in-order stream has near-zero jitter; an OOO retransmission with an old
// timestamp must leave the EWMA unchanged.
func TestPacketJitter_SkipsOutOfOrder(t *testing.T) {
	stats := NewFrameStatistics(128)
	pd := vp9.PayloadDescriptor{SID: 2, TID: 1}

	baseTime := time.Now().UnixMicro()
	const (
		frameTimeUS = int64(66_667) // 15fps
		tpf         = uint32(6000)  // 90kHz, 15fps
		ppf         = 7             // packets per frame
		pktTimeUS   = frameTimeUS / ppf
	)

	var seqNo uint16
	var rtpTS uint32 = 90000

	// Feed 60 in-order frames so the EWMA converges.
	for f := 0; f < 60; f++ {
		for p := 0; p < ppf; p++ {
			t_ := baseTime + int64(f)*frameTimeUS + int64(p)*pktTimeUS
			stats.AcceptPacket(t_, seqNo, rtpTS, 1200, &pd)
			seqNo++
		}
		rtpTS += tpf
	}

	var beforeSample VideoQualitySample
	stats.TakeSample(&beforeSample)

	// Inject a NACK retransmission: same RTP timestamp as a frame from 2 seconds ago.
	retransRTPTS := rtpTS - tpf*30 // 30 frames ago
	retransArrival := baseTime + 61*frameTimeUS
	stats.AcceptPacket(retransArrival, seqNo, retransRTPTS, 1200, &pd)
	seqNo++

	// Feed two more in-order frames so TakeSample has fresh data.
	for f := 60; f < 62; f++ {
		for p := 0; p < ppf; p++ {
			t_ := baseTime + int64(f)*frameTimeUS + int64(p)*pktTimeUS
			stats.AcceptPacket(t_, seqNo, rtpTS, 1200, &pd)
			seqNo++
		}
		rtpTS += tpf
	}

	var afterSample VideoQualitySample
	stats.TakeSample(&afterSample)

	// The retransmission must not have spiked packet jitter.
	// Before: converged to ~10ms baseline. After: must still be < 50ms.
	if afterSample.PacketJitterUS > 50_000 {
		t.Fatalf("OOO retransmission inflated PacketJitterUS: before=%.1f after=%.1f (want < 50000)",
			beforeSample.PacketJitterUS, afterSample.PacketJitterUS)
	}
}

func BenchmarkAcceptPacket(b *testing.B) {
	benchmarks := []struct {
		name       string
		bufferSize int
	}{
		{"bufferSize=128", 128},
		{"bufferSize=768", DefaultStatsBufferSize},
		{"bufferSize=4096", 4096},
	}
	testPackets := createTestPackets1080p25fps(60)
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			stats := NewFrameStatistics(bm.bufferSize)
			pd := &testPackets[0].payloadDesc
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pkt := &testPackets[i%len(testPackets)]
				stats.AcceptPacket(pkt.clientReadTime, pkt.seqNo, pkt.rtpTimestamp, pkt.nBytes, pd)
			}
		})
	}
}

func BenchmarkAcceptPacket_WithLoss(b *testing.B) {
	testPackets := createTestPackets1080p25fps(60)
	stats := NewFrameStatistics(DefaultStatsBufferSize)
	pd := &testPackets[0].payloadDesc

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// skip every 50th packet to simulate loss
		if i%50 == 0 {
			continue
		}
		pkt := &testPackets[i%len(testPackets)]
		stats.AcceptPacket(pkt.clientReadTime, pkt.seqNo, pkt.rtpTimestamp, pkt.nBytes, pd)
	}
}

func ExampleVideoQualitySample_Mbps() {
	sample := VideoQualitySample{SmoothBitrate: 3_500_000}
	fmt.Println(sample.Mbps())
	// Output: 3.50 Mbps
}
