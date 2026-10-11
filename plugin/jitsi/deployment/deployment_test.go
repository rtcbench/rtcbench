package deployment

import (
	"regexp"
	"strings"
	"testing"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

func TestWebRoutesColibriToLocalBridge(t *testing.T) {
	web := jitsiWeb(deploy.Config{Address: "10.2.96.10", Ports: map[string]int{"signaling": 1443}})
	env := web["environment"].(object)
	if env["ENABLE_COLIBRI_WEBSOCKET"] != "1" {
		t.Fatal("web proxy must enable Colibri WebSockets")
	}
	route := regexp.MustCompile("^" + env["COLIBRI_WEBSOCKET_REGEX"].(string) + "$")
	if !route.MatchString("127.0.0.1") || route.MatchString("127x0x0x1") || route.MatchString("10.2.96.10") {
		t.Fatal("Colibri proxy must match only the local bridge")
	}
}

func TestRecipeUsesCustomPorts(t *testing.T) {
	services, files := Definition.Recipe(deploy.Config{
		Address: "192.0.2.10",
		Ports:   map[string]int{"http": 1080, "signaling": 1443, "media": 11000},
	})
	config := string(files["jitsi-web.conf"])
	for _, line := range []string{"listen 1443 ssl", "listen 1080;"} {
		if !strings.Contains(config, line) {
			t.Errorf("web config is missing %q: %s", line, config)
		}
	}
	env := services["jvb"]["environment"].(object)
	for key, want := range map[string]string{"JVB_PORT": "11000", "JVB_WS_DOMAIN": "192.0.2.10:1443"} {
		if got := env[key]; got != want {
			t.Errorf("%s = %v, want %s", key, got, want)
		}
	}
	if got := services["web"]["environment"].(object)["PUBLIC_URL"]; got != "https://192.0.2.10:1443" {
		t.Errorf("PUBLIC_URL = %v", got)
	}
	probe := services["web"]["healthcheck"].(object)["test"].([]string)[1]
	if !strings.Contains(probe, "localhost:1443/") {
		t.Errorf("health check uses the wrong port: %s", probe)
	}
}
