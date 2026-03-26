package gcc

import (
	"sync/atomic"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
)

// TWCCExtFactory adds transport-cc sequence numbers to outgoing RTP
// packets so GCC can track them.
type TWCCExtFactory struct{}

func (TWCCExtFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	return &twccExtInterceptor{}, nil
}

type twccExtInterceptor struct {
	interceptor.NoOp
	seqNo atomic.Uint32
}

func (t *twccExtInterceptor) BindLocalStream(
	info *interceptor.StreamInfo, writer interceptor.RTPWriter,
) interceptor.RTPWriter {
	var extID uint8
	for _, ext := range info.RTPHeaderExtensions {
		if ext.URI == sdp.TransportCCURI {
			extID = uint8(ext.ID)
			break
		}
	}
	if extID == 0 {
		return writer
	}

	return interceptor.RTPWriterFunc(func(
		header *rtp.Header, payload []byte, attributes interceptor.Attributes,
	) (int, error) {
		seq := uint16(t.seqNo.Add(1) - 1)
		tcc := rtp.TransportCCExtension{TransportSequence: seq}
		raw, err := tcc.Marshal()
		if err != nil {
			return 0, err
		}
		if err := header.SetExtension(extID, raw); err != nil {
			return 0, err
		}
		return writer.Write(header, payload, attributes)
	})
}
