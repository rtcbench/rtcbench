package deployment

import (
	"strings"
	"testing"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

func TestRecipeUsesCustomPorts(t *testing.T) {
	services, _ := Definition.Recipe(deploy.Config{Ports: map[string]int{
		"signaling": 5443, "media": 45444,
	}})
	env := services["mediasoup"]["environment"].(object)
	for key, want := range map[string]string{"HTTP_PORT": "5443", "MEDIASOUP_MEDIA_PORT": "45444"} {
		if got := env[key]; got != want {
			t.Errorf("%s = %v, want %s", key, got, want)
		}
	}
	probe := services["mediasoup"]["healthcheck"].(object)["test"].([]string)[1]
	if !strings.Contains(probe, "localhost:5443/") {
		t.Errorf("health check uses the wrong port: %s", probe)
	}
}
