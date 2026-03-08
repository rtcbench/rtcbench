package cc

import pionrtcp "github.com/pion/rtcp"

// Controller is the interface for a congestion-control algorithm.
// DrainSender feeds it RTCP feedback; LoopIntoTrack queries it for the target rate.
type Controller interface {
	// OnREMB is called when the remote sends a Receiver Estimated Maximum Bitrate packet.
	OnREMB(bps uint64)
	// OnTWCC is called when a Transport-Wide Congestion Control feedback packet arrives.
	OnTWCC(pkt *pionrtcp.TransportLayerCC)
	// TargetBitrate returns the current send target in bits per second.
	// 0 means unconstrained — send at the natural IVF rate.
	TargetBitrate() uint64
}
