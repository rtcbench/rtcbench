package viewer

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"call.zip/pkg/pcap"
	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	// DefaultPacketsPerSample is the default number of packets between stats
	// samples. LiveKit overrides this with 200 to match its lower-bitrate SVC
	// configuration; all other plugins use the default.
	DefaultPacketsPerSample = 1000

	// DefaultTrackBufferSize is the byte buffer length for reading RTP packets
	// from a track. Shared across all plugins.
	DefaultTrackBufferSize = 1500

	// DefaultPLIMinIntervalNS is the minimum nanoseconds between PLI sends
	// (100 ms). Shared across all plugins.
	DefaultPLIMinIntervalNS = 100_000_000
)

// Viewer connects to a conference and receives 1 VP9 video stream
type Viewer struct {
	track        *webrtc.TrackRemote
	receiver     *webrtc.RTPReceiver
	input        chan<- vp9_stats.VideoQualitySample
	nickname     string
	config       Config
	ivf          *vp9.IvfSegmenter
	pcap         *pcap.Writer
	pub          *vp9_stats.Publisher
	rtcpTracker  *vp9_stats.RTCPTracker
	onFrameLost  func(nowNano int64)
}

type Config struct {
	PacketsPerSample  int    `json:"packets_per_sample"`
	VP9RTPPayloadType int    `json:"vp9_rtp_payload_type"`
	TrackBufferSize   int    `json:"track_buffer_size"`
	StatsBufferSize   int    `json:"stats_buffer_size"`
	PacketCaptureDir  string `json:"packet_capture_dir"` // if non-empty, write pcap file here
}

func (c *Config) verify() error {
	if c.VP9RTPPayloadType < 0 || c.VP9RTPPayloadType > 255 {
		return fmt.Errorf("invalid config: VP9RTPPayloadType (%d) out of range [0..255]",
			c.VP9RTPPayloadType)
	}
	if c.TrackBufferSize < 100 || c.TrackBufferSize > 10_000 { // TODO artificial limit
		return fmt.Errorf("invalid config: TrackBufferSize (%d) out of range [100..10000]", c.TrackBufferSize)
	}
	if c.StatsBufferSize < 64 || c.StatsBufferSize > 4096 {
		return fmt.Errorf("invalid config: StatsBufferSize (%d) out of range [64..4096]", c.StatsBufferSize)
	}
	return nil
}

func newViewer(
	track *webrtc.TrackRemote,
	receiver *webrtc.RTPReceiver,
	input chan<- vp9_stats.VideoQualitySample,
	nickname string,
	config *Config,
	ivf *vp9.IvfSegmenter,
	pub *vp9_stats.Publisher,
	rtcpTracker *vp9_stats.RTCPTracker,
	onFrameLost func(nowNano int64),
) (*Viewer, error) {
	if err := config.verify(); err != nil {
		return nil, err
	}
	v := &Viewer{
		track:       track,
		receiver:    receiver,
		input:       input,
		nickname:    nickname,
		config:      *config,
		ivf:         ivf,
		pub:         pub,
		rtcpTracker: rtcpTracker,
		onFrameLost: onFrameLost,
	}
	if config.PacketCaptureDir != "" {
		trackNickname := fmt.Sprintf("%s[%d]", nickname, track.SSRC())
		pcapPath := filepath.Join(config.PacketCaptureDir, trackNickname+".pcap")
		pw, err := pcap.New(pcapPath)
		if err != nil {
			return nil, fmt.Errorf("pcap writer: %w", err)
		}
		v.pcap = pw
	}
	return v, nil
}

func (v *Viewer) run(done <-chan struct{}) error {
	// Each incoming track has a unique SSRC; qualify the nickname so that a
	// receiver bot with multiple incoming streams produces a separate metrics
	// entry per stream rather than overwriting a single entry.
	trackNickname := fmt.Sprintf("%s[%d]", v.nickname, v.track.SSRC())

	var (
		pkt rtp.Packet
		buf = make([]byte, v.config.TrackBufferSize)
		raw []byte
		n   int
		seq uint16 = 0

		clientReadTime int64

		vp9RTPPayloadType = v.config.VP9RTPPayloadType
		vp9PayloadDesc    vp9.PayloadDescriptor
		vp9FrameStats    = vp9_stats.NewFrameStatistics(v.config.StatsBufferSize)
		vp9QualitySample vp9_stats.VideoQualitySample

		packetsInSample int
		packetsPerSample = v.config.PacketsPerSample

		err error
	)

	if v.onFrameLost != nil {
		vp9FrameStats.SetOnFrameLost(v.onFrameLost)
	}

loop:
	for {
		n, _, err = v.track.Read(buf)
		clientReadTime = time.Now().UnixMicro()

		if err != nil {
			select {
			case <-done:
				break loop
			default:
				v.pub.IncrError(vp9_stats.ErrViewerReadFromTrackID)
				continue
			}
		}

		raw = buf[:n]

		if v.pcap != nil {
			if err = v.pcap.WritePacket(raw, time.UnixMicro(clientReadTime)); err != nil {
				v.pub.IncrError(vp9_stats.ErrViewerPcapWriteID)
			}
		}

		if err = pkt.Unmarshal(raw); err != nil {
			v.pub.IncrError(vp9_stats.ErrViewerUnmarshalPacketID)
			continue
		}

		packetsInSample++

		if seq != 0 && pkt.SequenceNumber != seq+1 {
			v.pub.IncrError(vp9_stats.ErrViewerSeqNoJumpID)
			// fall-through
		}

		seq = pkt.SequenceNumber

		if int(pkt.PayloadType) != vp9RTPPayloadType {
			continue
		}

		if err = vp9.ParseVP9PayloadDescriptor(pkt.Payload, &vp9PayloadDesc); err != nil {
			v.pub.IncrError(vp9_stats.ErrViewerParseVP9PayloadID)
			continue
		}

		vp9FrameStats.AcceptPacket(clientReadTime, pkt.SequenceNumber, pkt.Timestamp, len(pkt.Payload), &vp9PayloadDesc)

		// TODO: map[uint32]*vp9.IvfSegmenter to split by SSRC
		if v.ivf != nil {
			if err = v.ivf.Push(pkt.Payload, pkt.Timestamp, &vp9PayloadDesc); err != nil {
				v.pub.IncrError(vp9_stats.ErrViewerIVFSegmenterFailed)
				continue
			}
		}

		if packetsInSample == packetsPerSample {
			packetsInSample = 0
			vp9FrameStats.TakeSample(&vp9QualitySample)
			vp9QualitySample.Nickname = trackNickname
			if v.rtcpTracker != nil {
				vp9QualitySample.RTCP = v.rtcpTracker.Snapshot()
			}
			select {
			case v.input <- vp9QualitySample:
				vp9FrameStats.EndSample()
			default:
				// TODO: adaptively lower the sampling rate due to detected consumer bottleneck
				v.pub.IncrError(vp9_stats.ErrViewerInputChannelID)
			}
		}
	}

	if err != nil {
		return errors.Join(err, fmt.Errorf("total viewer errs: %v", v.pub.Errors()))
	}

	return nil
}

func (v *Viewer) stop() {
	if v.receiver != nil {
		_ = v.receiver.Stop()
	}
	if v.pcap != nil {
		_ = v.pcap.Close()
	}
}
