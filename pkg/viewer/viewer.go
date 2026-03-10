package viewer

import (
	"errors"
	"fmt"
	"time"

	"call.zip/pkg/vp9"
	"call.zip/pkg/vp9_stats"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

const (
	errViewerReadFromTrackID int = iota
	errViewerUnmarshalPacketID
	errViewerParseVP9PayloadID
	errViewerSeqNoJumpID
	errViewerInputChannelID
	errViewerIVFSegmenterFailed
	numViewerErrIDs
)

// Viewer connects to a conference and receives 1 VP9 video stream
type Viewer struct {
	track    *webrtc.TrackRemote
	receiver *webrtc.RTPReceiver
	input    chan<- vp9_stats.VideoQualitySample
	nickname string
	config   Config
	ivf      *vp9.IvfSegmenter
}

type Config struct {
	PacketsPerSample  int `json:"packets_per_sample"`
	VP9RTPPayloadType int `json:"vp9_rtp_payload_type"`
	TrackBufferSize   int `json:"track_buffer_size"`
}

func (c *Config) verify() error {
	if c.VP9RTPPayloadType < 0 || c.VP9RTPPayloadType > 255 {
		return fmt.Errorf("invalid config: VP9RTPPayloadType (%d) out of range [0..255]",
			c.VP9RTPPayloadType)
	}
	if c.TrackBufferSize < 100 || c.TrackBufferSize > 10_000 { // TODO artificial limit
		return fmt.Errorf("invalid config: TrackBufferSize (%d) out of range [100..10000]", c.TrackBufferSize)
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
) (*Viewer, error) {
	if err := config.verify(); err != nil {
		return nil, err
	}
	return &Viewer{
		track:    track,
		receiver: receiver,
		input:    input,
		nickname: nickname,
		config:   *config,
		ivf:      ivf,
	}, nil
}

func (v *Viewer) run(done <-chan struct{}) error {
	var (
		pkt rtp.Packet
		buf = make([]byte, v.config.TrackBufferSize)
		raw []byte
		n   int
		seq uint16 = 0

		clientReadTime int64

		vp9RTPPayloadType = v.config.VP9RTPPayloadType
		vp9PayloadDesc    vp9.PayloadDescriptor
		vp9FrameStats     vp9_stats.FrameStatistics
		vp9QualitySample  vp9_stats.VideoQualitySample

		packetsInSample  int
		packetsPerSample = v.config.PacketsPerSample

		err  error
		errs [numViewerErrIDs]int64
	)

loop:
	for {
		n, _, err = v.track.Read(buf)
		clientReadTime = time.Now().UnixMicro()

		//log.Println("packet: ", n)

		if err != nil {
			select {
			case <-done:
				break loop
			default:
				errs[errViewerReadFromTrackID]++
				continue
			}
		}

		raw = buf[:n]

		if err = pkt.Unmarshal(raw); err != nil {
			errs[errViewerUnmarshalPacketID]++
			continue
		}

		packetsInSample++

		if seq != 0 && pkt.SequenceNumber != seq+1 {
			errs[errViewerSeqNoJumpID]++
			// fall-through
		}

		seq = pkt.SequenceNumber

		if int(pkt.PayloadType) != vp9RTPPayloadType {
			continue
		}

		if err = vp9.ParseVP9PayloadDescriptor(pkt.Payload, &vp9PayloadDesc); err != nil {
			errs[errViewerParseVP9PayloadID]++
			continue
		}

		vp9FrameStats.AcceptPacket(clientReadTime, pkt.Timestamp, len(pkt.Payload), &vp9PayloadDesc)

		// TODO: map[uint32]*vp9.IvfSegmenter to split by SSRC
		if v.ivf != nil {
			if err = v.ivf.Push(pkt.Payload, pkt.Timestamp, &vp9PayloadDesc); err != nil {
				errs[errViewerIVFSegmenterFailed]++
				continue
			}
		}

		if packetsInSample == packetsPerSample {
			packetsInSample = 0
			vp9FrameStats.TakeSample(&vp9QualitySample)
			vp9QualitySample.Nickname = v.nickname
			select {
			case v.input <- vp9QualitySample:
				vp9FrameStats.EndSample()
			default:
				// TODO: adaptively lower the sampling rate due to detected consumer bottleneck
				errs[errViewerInputChannelID]++
			}
		}
	}

	if err != nil {
		return joinErrors(err, errs)
	}

	return nil
}

func (v *Viewer) stop() {
	if v.receiver != nil {
		_ = v.receiver.Stop()
	}
}

func joinErrors(err error, errs [numViewerErrIDs]int64) error {
	// TODO friendlier error message than an array of counts
	return errors.Join(err, fmt.Errorf("total viewer errs: %v", errs))
}
