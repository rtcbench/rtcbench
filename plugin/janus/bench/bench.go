package bench

import (
	"context"
	"net/url"
	"strconv"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

type Config struct {
	JanusAPI int `yaml:"janus_api_port" json:"janus_api_port"`
	JanusWeb int `yaml:"janus_web_port" json:"janus_web_port"`
}

func DefaultConfig() Config { return Config{JanusAPI: 8088, JanusWeb: 8080} }

func New(e benchmark.Environment, c Config) benchmark.Plugin {
	return benchmark.Plugin{
		ID:             "janus",
		Host:           e.Host(),
		Hosts:          e.SFU,
		SenderRoomSize: 65,
		Ports: []benchmark.Port{
			{Value: c.JanusAPI, Default: 8088},
			{Value: c.JanusWeb, Default: 8080},
		},
		NativeConfig: func(ip string) map[string]any {
			return map[string]any{
				"_serverRoot":        benchmark.Address("http", ip, c.JanusAPI) + "/janus",
				"allowInsecureHttps": false,
			}
		},
		BrowserURL: func(ip string, publish bool) string {
			params := url.Values{
				"room":    {"1234"},
				"server":  {benchmark.Address("http", ip, c.JanusAPI) + "/janus"},
				"publish": {strconv.FormatBool(publish)},
				"receive": {strconv.FormatBool(!publish)},
			}
			return benchmark.Address("http", ip, c.JanusWeb) + "/?" + params.Encode()
		},
		Services: []benchmark.Service{
			{
				Host:   e.Host(),
				Name:   e.Name("sfu"),
				Image:  "rtcbench-janus:latest",
				Health: "curl -fsS --max-time 2 http://localhost:8088/janus/info",
			},
			{
				Host:  e.Host(),
				Name:  e.Name("web"),
				Image: "rtcbench-janus-web:latest",
			},
		},
		PrepareRooms: func(ctx context.Context, remote benchmark.Remote, count int) error {
			if count <= 1 {
				return nil
			}
			return prepareRooms(ctx, remote, e.Host(), c.JanusAPI, count)
		},
	}
}
