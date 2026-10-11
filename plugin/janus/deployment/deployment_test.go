package deployment

import (
	"strings"
	"testing"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

func TestRecipeUsesCustomPorts(t *testing.T) {
	services, files := Definition.Recipe(deploy.Config{Ports: map[string]int{
		"signaling": 9088, "udpStart": 11000, "udpEnd": 21000,
	}})
	if config := string(files["janus-http.jcfg"]); !strings.Contains(config, "port = 9088") {
		t.Errorf("HTTP config uses the wrong port: %s", config)
	}
	command := strings.Join(services["janus"]["command"].([]string), " ")
	if !strings.Contains(command, "--rtp-port-range=11000-21000") {
		t.Errorf("command uses the wrong RTP range: %s", command)
	}
	probe := services["janus"]["healthcheck"].(object)["test"].([]string)[1]
	if !strings.Contains(probe, "localhost:9088/") {
		t.Errorf("health check uses the wrong port: %s", probe)
	}
}
