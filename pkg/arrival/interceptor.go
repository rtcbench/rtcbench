// Package arrival provides a pion interceptor that timestamps incoming RTP
// packets at the network layer (before pion's internal jitter buffer). This
// gives callers access to true network arrival times instead of the
// Track.Read() return time, which is smoothed by internal buffering.
package arrival

import (
	"time"

	"github.com/pion/interceptor"
)

// AttrKeyArrivalTimeUS is the interceptor attribute key for the packet arrival
// time in Unix microseconds.
const AttrKeyArrivalTimeUS = "arrival_us"

type timestampInterceptor struct {
	interceptor.NoOp
}

func (t *timestampInterceptor) BindRemoteStream(
	_ *interceptor.StreamInfo, reader interceptor.RTPReader,
) interceptor.RTPReader {
	return interceptor.RTPReaderFunc(func(buf []byte, attrs interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, attrs, err := reader.Read(buf, attrs)
		if err == nil {
			if attrs == nil {
				attrs = make(interceptor.Attributes)
			}
			attrs.Set(AttrKeyArrivalTimeUS, time.Now().UnixMicro())
		}
		return n, attrs, err
	})
}

// Factory creates timestampInterceptor instances.
type Factory struct{}

func (f Factory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	return &timestampInterceptor{}, nil
}
