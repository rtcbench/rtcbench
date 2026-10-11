package deployment

import (
	"strings"
	"testing"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

func TestRecipeUsesCustomPorts(t *testing.T) {
	services, files := Definition.Recipe(deploy.Config{Ports: map[string]int{
		"signaling": 8880, "rtcTcp": 8881, "udpStart": 11000, "udpEnd": 61000,
	}})
	config := string(files["livekit-runtime.yaml"])
	for _, line := range []string{"port: 8880", "tcp_port: 8881", "port_range_start: 11000", "port_range_end: 61000"} {
		if !strings.Contains(config, line) {
			t.Errorf("runtime config is missing %q: %s", line, config)
		}
	}
	probe := services["livekit"]["healthcheck"].(object)["test"].([]string)[1]
	if !strings.Contains(probe, "localhost:8880/") {
		t.Errorf("health check uses the wrong port: %s", probe)
	}
}
