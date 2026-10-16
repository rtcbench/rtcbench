package bench

import (
	"fmt"
	"net/url"
	"strconv"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

type Config struct {
	MediasoupWS  int `yaml:"mediasoup_ws_port" json:"mediasoup_ws_port"`
	MediasoupWeb int `yaml:"mediasoup_web_port" json:"mediasoup_web_port"`
}

func DefaultConfig() Config { return Config{MediasoupWS: 4443, MediasoupWeb: 8080} }

func New(e benchmark.Environment, c Config) benchmark.Plugin {
	return benchmark.Plugin{
		ID:             "mediasoup",
		Host:           e.Host(),
		Hosts:          e.SFU,
		SenderRoomSize: 50,
		Ports: []benchmark.Port{
			{Value: c.MediasoupWS, Default: 4443},
			{Value: c.MediasoupWeb, Default: 8080},
		},
		NativeConfig: func(ip string) map[string]any {
			return map[string]any{"_serverURL": benchmark.Address("wss", ip, c.MediasoupWS)}
		},
		BrowserURL: func(ip string, publish bool) string {
			params := url.Values{
				"roomId":      {"room-1234"},
				"produce":     {strconv.FormatBool(publish)},
				"consume":     {strconv.FormatBool(!publish)},
				"webcam":      {strconv.FormatBool(publish)},
				"mic":         {"false"},
				"forceVP9":    {"true"},
				"displayName": {"bench"},
				"publish":     {strconv.FormatBool(publish)},
				"receive":     {strconv.FormatBool(!publish)},
			}
			return benchmark.Address("https", ip, c.MediasoupWeb) + "/?" + params.Encode()
		},
		Services: []benchmark.Service{
			{
				Host:  e.Host(),
				Name:  e.Name("sfu"),
				Image: "rtcbench-mediasoup:latest",
				Health: fmt.Sprintf(
					"test \"$(curl -kSs --max-time 2 -o /dev/null -w '%%{http_code}' https://localhost:%d/)\" = 403",
					c.MediasoupWS,
				),
				Env: []string{
					"DOMAIN=" + e.DataIP(e.Host()),
					"MEDIASOUP_ANNOUNCED_ADDRESS=" + e.DataIP(e.Host()),
					"INITIAL_OUTGOING_BITRATE=500000000",
				},
			},
			{
				Host:  e.Host(),
				Name:  e.Name("web"),
				Image: "rtcbench-mediasoup-web:latest",
			},
		},
	}
}
