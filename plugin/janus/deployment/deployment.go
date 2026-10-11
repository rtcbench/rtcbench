package deployment

import (
	"fmt"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

type object = map[string]any

var Definition = deploy.Plugin{
	WebUI: &deploy.WebUI{Port: "signaling"},
	ID:    "janus",
	Name:  "Janus",
	Ports: []deploy.Port{
		{
			ID:       "signaling",
			Name:     "Signaling",
			Protocol: "TCP",
			Default:  8088,
		},
		{
			ID:       "udpStart",
			Name:     "Media range start",
			Protocol: "UDP",
			Default:  10000,
		},
		{
			ID:       "udpEnd",
			Name:     "Media range end",
			Protocol: "UDP",
			Default:  20000,
			MinFrom:  "udpStart",
		},
	},
	Bind:   bind,
	Recipe: janusRecipe,
}

func bind(d deploy.Config) (map[string]any, map[string]string) {
	return map[string]any{"serverRoot": fmt.Sprintf("http://%s:%d/janus", d.Address, d.Ports["signaling"])}, nil
}

func janusRecipe(d deploy.Config) (map[string]object, map[string][]byte) {
	p := d.Ports
	s := deploy.BuiltService(d, fmt.Sprintf("curl -fsS http://localhost:%d/janus/info >/dev/null", p["signaling"]))
	s["command"] = []string{
		"janus", "--log-stdout", "--nat-1-1=" + d.Address,
		fmt.Sprintf("--rtp-port-range=%d-%d", p["udpStart"], p["udpEnd"]),
	}
	s["volumes"] = []string{"./janus-http.jcfg:/opt/janus/etc/janus/janus.transport.http.jcfg:ro"}
	config := fmt.Sprintf(`general: {
  json = "indented";
  base_path = "/janus";
  threads = "unlimited";
  http = true;
  port = %d;
  https = false;
}
admin: {
  admin_http = false;
  admin_https = false;
}
`, p["signaling"])
	return map[string]object{"janus": s}, map[string][]byte{"janus-http.jcfg": []byte(config)}
}
