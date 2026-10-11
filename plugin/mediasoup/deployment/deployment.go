package deployment

import (
	"fmt"
	"strings"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

type object = map[string]any

var Definition = deploy.Plugin{
	WebUI: &deploy.WebUI{Port: "signaling", HTTPS: true},
	ID:    "mediasoup",
	Name:  "mediasoup",
	Ports: []deploy.Port{
		{
			ID:       "signaling",
			Name:     "Signaling",
			Protocol: "TCP",
			Default:  4443,
		},
		{
			ID:       "media",
			Name:     "First worker",
			Protocol: "TCP/UDP",
			Default:  44444,
		},
	},
	Bind:          bind,
	Recipe:        mediasoupRecipe,
	ValidatePorts: validatePorts,
	CheckHost:     checkHost,
	PortNote:      "Uses consecutive TCP and UDP ports starting here, one per CPU worker. Keep signaling outside that range.",
}

func bind(d deploy.Config) (map[string]any, map[string]string) {
	return map[string]any{"serverURL": fmt.Sprintf("wss://%s:%d", d.Address, d.Ports["signaling"])}, nil
}

func validatePorts(ports map[string]int, _ bool) error {
	return validateWorkerPorts(ports, 1)
}

func checkHost(d deploy.Config, run func(string) (string, error)) error {
	output, err := run("getconf _NPROCESSORS_ONLN")
	if err != nil {
		return err
	}
	var workers int
	if _, err := fmt.Sscan(strings.TrimSpace(output), &workers); err != nil {
		return err
	}
	return validateWorkerPorts(d.Ports, workers)
}

func validateWorkerPorts(ports map[string]int, workers int) error {
	first, signaling := ports["media"], ports["signaling"]
	last := first + workers - 1
	if workers < 1 || workers > 65535 || last > 65535 || (signaling >= first && signaling <= last) {
		return fmt.Errorf("media ports %d-%d for %d workers must fit within 1-65535 and not overlap signaling port %d", first, last, workers, signaling)
	}
	return nil
}

func mediasoupRecipe(d deploy.Config) (map[string]object, map[string][]byte) {
	// The signaling server may return 404 at /; match the bundled image's probe
	s := deploy.BuiltService(d, fmt.Sprintf("curl -ksS https://localhost:%d/ >/dev/null", d.Ports["signaling"]))
	s["environment"] = object{
		"DOMAIN":                      d.Address,
		"MEDIASOUP_ANNOUNCED_ADDRESS": d.Address,
		"HTTP_PORT":                   fmt.Sprint(d.Ports["signaling"]),
		"MEDIASOUP_MEDIA_PORT":        fmt.Sprint(d.Ports["media"]),
	}
	return map[string]object{"mediasoup": s}, map[string][]byte{}
}
