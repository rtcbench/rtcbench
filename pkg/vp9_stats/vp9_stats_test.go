package vp9_stats

import (
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

	// clientReadTime ends just before current time, so all tests will have different times
	clientReadTime := time.Now().Add(time.Duration(-seconds)*time.Second - 1*time.Second)

	for i := range nPackets {
		ppf_ := ppf
		if i%int(ppf_) == 0 {
			frameRTPTimestamp = rtpTimestamp
		}

		pkts[i] = testPacket{
			clientReadTime: clientReadTime.UnixMicro(),
			rtpTimestamp:   frameRTPTimestamp,
			nBytes:         int(size),
			payloadDesc: vp9.PayloadDescriptor{
				SID: maxLayerSpatialID,
				TID: maxLayerTemporalID,
			},
		}
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
			stats.AcceptPacket(pkt.clientReadTime, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
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
			stats.AcceptPacket(pkt.clientReadTime, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
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
		stats.AcceptPacket(pkt.clientReadTime, pkt.rtpTimestamp, pkt.nBytes, &pkt.payloadDesc)
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

	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	// Feed 63 packets (one less than bufferSize)
	for i := 0; i < bufferSize-1; i++ {
		stats.AcceptPacket(baseTime+int64(i)*1000, rtpTS, 100, &pd)
		rtpTS += 3600 // increment per packet
	}

	if stats.Initialized() {
		t.Fatalf("expected Initialized() == false after %d packets (bufferSize=%d)", bufferSize-1, bufferSize)
	}

	// Feed the 64th packet
	stats.AcceptPacket(baseTime+int64(bufferSize-1)*1000, rtpTS, 100, &pd)

	if !stats.Initialized() {
		t.Fatalf("expected Initialized() == true after %d packets (bufferSize=%d)", bufferSize, bufferSize)
	}
}

func TestFrameStatistics_TakeSample_BeforeInitialized(t *testing.T) {
	stats := NewFrameStatistics(DefaultStatsBufferSize)

	baseTime := time.Now().UnixMicro()
	var rtpTS uint32 = 5000

	pd := vp9.PayloadDescriptor{
		SID: maxLayerSpatialID,
		TID: maxLayerTemporalID,
	}

	// Feed only 10 packets (well below the 768 buffer size)
	for i := 0; i < 10; i++ {
		stats.AcceptPacket(baseTime+int64(i)*1000, rtpTS, 200, &pd)
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
				stats.AcceptPacket(pkt.clientReadTime, pkt.rtpTimestamp, pkt.nBytes, pd)
			}
		})
	}
}
