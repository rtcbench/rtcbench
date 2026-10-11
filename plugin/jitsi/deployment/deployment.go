package deployment

import (
	"bytes"
	"fmt"
	"maps"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

type object = map[string]any

var Definition = deploy.Plugin{
	WebUI: &deploy.WebUI{Port: "http", Join: func(d deploy.Config, _, _ string) (map[string]string, error) {
		return map[string]string{"domain": d.Address}, nil
	}, Rewrite: func(d deploy.Config, body []byte, proxy string) []byte {
		return bytes.ReplaceAll(body, []byte(fmt.Sprintf("wss://%s:%d/colibri-ws/", d.Address, d.Ports["signaling"])), []byte(proxy+"colibri-ws/"))
	}},
	ID:   "jitsi",
	Name: "Jitsi",
	Ports: []deploy.Port{
		{
			ID:       "http",
			Name:     "HTTP",
			Protocol: "TCP",
			Default:  80,
		},
		{
			ID:       "signaling",
			Name:     "HTTPS / signaling",
			Protocol: "TCP",
			Default:  443,
		},
		{
			ID:       "media",
			Name:     "Media",
			Protocol: "UDP",
			Default:  10000,
		},
	},
	Bind:            bind,
	Recipe:          jitsiRecipe,
	ValidatePorts:   validatePorts,
	TerminalService: "jvb",
	PortNote:        "Managed deployments reserve internal TCP ports 5222, 5280, 8080, 8888 and 9090.",
}

func bind(d deploy.Config) (map[string]any, map[string]string) {
	return map[string]any{
		"boshURL": fmt.Sprintf("https://%s:%d/http-bind", d.Address, d.Ports["signaling"]),
		"domain":  d.Address,
	}, nil
}

func validatePorts(ports map[string]int, managed bool) error {
	if ports["http"] == ports["signaling"] {
		return fmt.Errorf("TCP listeners must use different ports")
	}
	if !managed {
		return nil
	}
	for _, reserved := range []int{5222, 5280, 8080, 8888, 9090} {
		if ports["http"] == reserved || ports["signaling"] == reserved {
			return fmt.Errorf("TCP %d is reserved for an internal Jitsi service", reserved)
		}
	}
	return nil
}

func jitsiEnvironment(d deploy.Config) object {
	return object{
		"AUTH_TYPE":                "internal",
		"ENABLE_AUTH":              "0",
		"ENABLE_GUESTS":            "1",
		"XMPP_DOMAIN":              d.Address,
		"XMPP_AUTH_DOMAIN":         "auth." + d.Address,
		"XMPP_MUC_DOMAIN":          "conference." + d.Address,
		"XMPP_INTERNAL_MUC_DOMAIN": "internal-muc." + d.Address,
		"XMPP_RECORDER_DOMAIN":     "recorder." + d.Address,
		"XMPP_SERVER":              "127.0.0.1",
		"XMPP_PORT":                "5222",
		"JICOFO_AUTH_PASSWORD":     d.Secret,
		"JVB_AUTH_PASSWORD":        d.Secret,
		"TZ":                       "UTC",
	}
}

func jitsiService(d deploy.Config, name, probe string, extra object) object {
	s := deploy.Service("jitsi/"+name+":stable-9646", probe)
	env := jitsiEnvironment(d)
	maps.Copy(env, extra)
	s["environment"] = env
	return s
}

func jitsiRecipe(d deploy.Config) (map[string]object, map[string][]byte) {
	services := map[string]object{
		"prosody": jitsiService(d, "prosody", "timeout 3 bash -c 'echo > /dev/tcp/localhost/5222'", nil),
		"jicofo": jitsiService(d, "jicofo", "curl -fsS http://localhost:8888/about/health >/dev/null", object{
			"JICOFO_AUTH_USER":            "focus",
			"JICOFO_ENABLE_HEALTH_CHECKS": "true",
		}),
		"jvb": jitsiService(d, "jvb", "curl -fsS http://localhost:8080/about/health >/dev/null", object{
			"JVB_AUTH_USER":              "jvb",
			"JVB_ADVERTISE_IPS":          d.Address,
			"JVB_PORT":                   fmt.Sprint(d.Ports["media"]),
			"JVB_MUC_NICKNAME":           "rtcbench",
			"JVB_TCP_HARVESTER_DISABLED": "true",
			"JVB_STUN_SERVERS":           "",
			"ENABLE_COLIBRI_WEBSOCKET":   "1",
			"JVB_WS_DOMAIN":              fmt.Sprintf("%s:%d", d.Address, d.Ports["signaling"]),
			"JVB_WS_SERVER_ID":           "127.0.0.1",
		}),
		"web": jitsiWeb(d),
	}
	for _, name := range []string{"jicofo", "jvb"} {
		services[name]["depends_on"] = object{"prosody": object{"condition": "service_healthy"}}
	}
	config := fmt.Sprintf(`server {
 listen %d;
 include /config/nginx/meet.conf;
}
server {
 listen %d ssl http2;
 include /config/nginx/ssl.conf;
 include /config/nginx/meet.conf;
}
`, d.Ports["http"], d.Ports["signaling"])
	return services, map[string][]byte{"jitsi-web.conf": []byte(config)}
}

func jitsiWeb(d deploy.Config) object {
	web := jitsiService(d, "web", fmt.Sprintf("curl -kfsS https://localhost:%d/ >/dev/null", d.Ports["signaling"]), object{
		"ENABLE_COLIBRI_WEBSOCKET": "1",
		"COLIBRI_WEBSOCKET_REGEX":  `127\.0\.0\.1`,
		"XMPP_BOSH_URL_BASE":       "http://127.0.0.1:5280",
		"PUBLIC_URL":               fmt.Sprintf("https://%s:%d", d.Address, d.Ports["signaling"]),
	})
	web["volumes"] = []string{"./jitsi-web.conf:/defaults/default:ro"}
	web["image"] = "rtcbench-managed-jitsi:" + d.ID
	web["build"] = object{"context": "./jitsi"}
	web["depends_on"] = object{
		"jicofo": object{"condition": "service_healthy"},
		"jvb":    object{"condition": "service_healthy"},
	}
	return web
}
