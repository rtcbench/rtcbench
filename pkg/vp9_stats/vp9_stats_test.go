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
		var stats FrameStatistics
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

			if sample.SequenceNo < StatsBufferSize-1 {
				t.Fatalf("[seconds=%d] initialized seq no < StatsBufferSize", seconds)
			}

			if sample.EstimatedFPS != 25 {
				t.Fatalf("[seconds=%d;seq=%d] expected EstimatedFPS=25, got %v",
					seconds, sample.SequenceNo, sample.EstimatedFPS)
			}

			if sample.SVC.Layers[maxLayerSpatialID][maxLayerTemporalID] != StatsBufferSize {
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
