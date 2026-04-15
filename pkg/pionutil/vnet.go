package pionutil

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/transport/v4/vnet"
)

type ImpairmentProfile struct {
	Name         string
	BandwidthBps int
	BaseLatency  time.Duration
	JitterStddev time.Duration
	LossPercent  int
	Seed         int64
}

var virtualIPMu sync.Mutex
var virtualIPCounter = uint32(10)

func allocateVirtualIP() string {
	virtualIPMu.Lock()
	defer virtualIPMu.Unlock()
	n := virtualIPCounter
	virtualIPCounter++
	return fmt.Sprintf("203.0.113.%d", n)
}

func buildParticipantNet(router *vnet.Router, virtualIP string) (*vnet.Net, error) {
	net, err := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{virtualIP}})
	if err != nil {
		return nil, fmt.Errorf("vnet.NewNet: %w", err)
	}

	if err := router.AddNet(net); err != nil {
		return nil, fmt.Errorf("router.AddNet: %w", err)
	}
	return net, nil
}
