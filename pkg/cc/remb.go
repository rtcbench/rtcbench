package cc

import (
	"sync/atomic"

	pionrtcp "github.com/pion/rtcp"
)

// REMB is a Controller that obeys Receiver Estimated Maximum Bitrate signals.
// It is the simplest adaptive-bitrate strategy: trust the server's own estimate.
type REMB struct {
	target atomic.Uint64
}

func NewREMB() *REMB { return &REMB{} }

func (r *REMB) OnREMB(bps uint64)                    { r.target.Store(bps) }
func (r *REMB) OnTWCC(_ *pionrtcp.TransportLayerCC)  {}
func (r *REMB) TargetBitrate() uint64                { return r.target.Load() }
