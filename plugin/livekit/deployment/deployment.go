package deployment

import (
	"fmt"
	"github.com/livekit/protocol/auth"
	"time"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

type object = map[string]any

var Definition = deploy.Plugin{
	WebUI: &deploy.WebUI{Port: "signaling", Join: func(d deploy.Config, room, identity string) (map[string]string, error) {
		token, err := auth.NewAccessToken(d.APIKey, d.Secret).SetIdentity(identity).SetValidFor(10 * time.Minute).SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: room}).ToJWT()
		return map[string]string{"token": token}, err
	}},
	ID:   "livekit",
	Name: "LiveKit",
	Ports: []deploy.Port{
		{
			ID:       "signaling",
			Name:     "Signaling",
			Protocol: "TCP",
			Default:  7880,
		},
		{
			ID:       "rtcTcp",
			Name:     "RTC",
			Protocol: "TCP",
			Default:  7881,
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
			Default:  60000,
			MinFrom:  "udpStart",
		},
	},
	Bind:          bind,
	Recipe:        livekitRecipe,
	ValidatePorts: validatePorts,
	Credentials:   true,
}

func bind(d deploy.Config) (map[string]any, map[string]string) {
	return map[string]any{
		"wsURL":     fmt.Sprintf("ws://%s:%d", d.Address, d.Ports["signaling"]),
		"apiKey":    "$env:SFU_API_KEY",
		"apiSecret": "$env:SFU_API_SECRET",
	}, map[string]string{"SFU_API_KEY": d.APIKey, "SFU_API_SECRET": d.Secret}
}

func validatePorts(ports map[string]int, _ bool) error {
	if ports["signaling"] == ports["rtcTcp"] {
		return fmt.Errorf("TCP listeners must use different ports")
	}
	return nil
}

func livekitRecipe(d deploy.Config) (map[string]object, map[string][]byte) {
	p := d.Ports
	s := deploy.BuiltService(d, fmt.Sprintf("curl -fsS http://localhost:%d/ >/dev/null", p["signaling"]))
	s["volumes"] = []string{"./livekit-runtime.yaml:/etc/livekit.yaml:ro"}
	config := fmt.Sprintf(`port: %d
rtc:
  tcp_port: %d
  port_range_start: %d
  port_range_end: %d
  use_external_ip: false
  node_ip: %s
keys:
  rtcbench: %s
room:
  auto_create: true
  departure_timeout: 5
  empty_timeout: 10
`, p["signaling"], p["rtcTcp"], p["udpStart"], p["udpEnd"], d.Address, d.Secret)
	return map[string]object{"livekit": s}, map[string][]byte{"livekit-runtime.yaml": []byte(config)}
}
