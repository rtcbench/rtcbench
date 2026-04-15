package pionutil

import (
	"testing"

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

func TestBuildParticipantNet(t *testing.T) {
	router, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:          "test",
		CIDR:          "203.0.113.0/24",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Stop()

	net, err := buildParticipantNet(router, "203.0.113.10")
	if err != nil {
		t.Fatalf("buildParticipantNet: %v", err)
	}
	if net == nil {
		t.Fatal("expected non-nil Net")
	}
}
