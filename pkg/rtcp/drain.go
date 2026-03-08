package rtcp

import (
	"call.zip/pkg/cc"
	pionrtcp "github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

// DrainSender reads RTCP from a sender for the lifetime of the connection.
// Must be called in a goroutine. Without it the sender's RTCP buffer fills
// and Pion's interceptor pipeline stalls.
//
// If ctrl is non-nil, REMB and TWCC packets are forwarded to the controller
// so it can adjust the target bitrate consumed by LoopIntoTrack.
func DrainSender(logf func(string, ...any), sender *webrtc.RTPSender, ctrl cc.Controller) {
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, pkt := range pkts {
			switch p := pkt.(type) {
			case *pionrtcp.PictureLossIndication:
				logf("[rtcp] PLI — server requesting keyframe")
			case *pionrtcp.FullIntraRequest:
				logf("[rtcp] FIR — server requesting keyframe")
			case *pionrtcp.ReceiverEstimatedMaximumBitrate:
				logf("[rtcp] REMB — server estimate: %d bps", p.Bitrate)
				if ctrl != nil {
					ctrl.OnREMB(uint64(p.Bitrate))
				}
			case *pionrtcp.TransportLayerCC:
				if ctrl != nil {
					ctrl.OnTWCC(p)
				}
			}
		}
	}
}
