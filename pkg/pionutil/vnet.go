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

func buildParticipantNet(router *vnet.Router, p *ImpairmentProfile, virtualIP string) (*vnet.Net, error) {
	net, err := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{virtualIP}})
	if err != nil {
		return nil, fmt.Errorf("vnet.NewNet: %w", err)
	}

	var wrapped vnet.NIC = net

	if p.BandwidthBps > 0 {
		burst := p.BandwidthBps / 8 / 10
		if burst < 1500 {
			burst = 1500
		}
		tbf := vnet.NewTBFQueue(p.BandwidthBps, burst, 1<<20)
		q, err := vnet.NewQueue(wrapped, tbf)
		if err != nil {
			return nil, fmt.Errorf("vnet.NewQueue: %w", err)
		}
		wrapped = q
	}

	if p.LossPercent > 0 {
		opts := []vnet.LossFilterOption{}
		if p.Seed != 0 {
			opts = append(opts, vnet.WithLossSeed(p.Seed))
		}
		lf, err := vnet.NewLossFilter(wrapped, p.LossPercent, opts...)
		if err != nil {
			return nil, fmt.Errorf("vnet.NewLossFilter: %w", err)
		}
		wrapped = lf
	}

	if p.BaseLatency > 0 {
		df, err := vnet.NewDelayFilter(wrapped, vnet.WithDelay(p.BaseLatency))
		if err != nil {
			return nil, fmt.Errorf("vnet.NewDelayFilter: %w", err)
		}
		wrapped = df
	}

	if err := router.AddNet(wrapped); err != nil {
		return nil, fmt.Errorf("router.AddNet: %w", err)
	}
	return net, nil
}
