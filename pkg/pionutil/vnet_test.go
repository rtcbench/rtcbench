package pionutil

import (
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v4/vnet"
)

func TestAllocateVirtualIPUniqueness(t *testing.T) {
	ips := make(map[string]bool)
	for i := 0; i < 100; i++ {
		ip := allocateVirtualIP()
		if ips[ip] {
			t.Errorf("duplicate IP allocated: %s", ip)
		}
		ips[ip] = true
	}
}

func TestBuildParticipantNetConstantDelay(t *testing.T) {
	router, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:          "test",
		CIDR:          "203.0.113.0/24",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Stop()

	profile := &ImpairmentProfile{
		Name:         "constant-delay",
		BandwidthBps: 0,
		BaseLatency:  50 * time.Millisecond,
		JitterStddev: 0,
		LossPercent:  0,
		Seed:         0,
	}

	net, err := buildParticipantNet(router, profile, "203.0.113.10")
	if err != nil {
		t.Fatalf("buildParticipantNet: %v", err)
	}

	if net == nil {
		t.Fatal("expected non-nil Net")
	}
}

func TestBuildParticipantNetWithLoss(t *testing.T) {
	router, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:          "test",
		CIDR:          "203.0.113.0/24",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Stop()

	profile := &ImpairmentProfile{
		Name:         "lossy",
		BandwidthBps: 0,
		BaseLatency:  0,
		JitterStddev: 0,
		LossPercent:  30,
		Seed:         1,
	}

	net, err := buildParticipantNet(router, profile, "203.0.113.11")
	if err != nil {
		t.Fatalf("buildParticipantNet: %v", err)
	}

	if net == nil {
		t.Fatal("expected non-nil Net")
	}
}

func TestBuildParticipantNetWithBandwidth(t *testing.T) {
	router, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:          "test",
		CIDR:          "203.0.113.0/24",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Stop()

	profile := &ImpairmentProfile{
		Name:         "bandwidth-limited",
		BandwidthBps: 1_000_000,
		BaseLatency:  0,
		JitterStddev: 0,
		LossPercent:  0,
		Seed:         0,
	}

	net, err := buildParticipantNet(router, profile, "203.0.113.12")
	if err != nil {
		t.Fatalf("buildParticipantNet: %v", err)
	}

	if net == nil {
		t.Fatal("expected non-nil Net")
	}
}
