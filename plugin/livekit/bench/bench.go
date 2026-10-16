package bench

import (
	"context"
	"net/url"
	"path"
	"path/filepath"
	"strconv"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

type Config struct {
	LiveKitWS  int `yaml:"livekit_ws_port" json:"livekit_ws_port"`
	LiveKitWeb int `yaml:"livekit_web_port" json:"livekit_web_port"`
}

func DefaultConfig() Config { return Config{LiveKitWS: 7880, LiveKitWeb: 8080} }

func New(e benchmark.Environment, c Config) benchmark.Plugin {
	return benchmark.Plugin{
		ID:             "livekit",
		Host:           e.Host(),
		Hosts:          e.SFU,
		SenderRoomSize: 65,
		Ports: []benchmark.Port{
			{Value: c.LiveKitWS, Default: 7880},
			{Value: c.LiveKitWeb, Default: 8080},
		},
		NativeConfig: func(ip string) map[string]any {
			return map[string]any{
				"_wsURL":    benchmark.Address("ws", ip, c.LiveKitWS),
				"apiKey":    "devkey",
				"apiSecret": "secret-secret-secret-secret-secret",
			}
		},
		BrowserURL: func(ip string, publish bool) string {
			params := url.Values{
				"ws":      {benchmark.Address("ws", ip, c.LiveKitWS)},
				"room":    {"room-1234"},
				"key":     {"devkey"},
				"secret":  {"secret-secret-secret-secret-secret"},
				"publish": {strconv.FormatBool(publish)},
				"receive": {strconv.FormatBool(!publish)},
			}
			return benchmark.Address("http", ip, c.LiveKitWeb) + "/?" + params.Encode()
		},
		Services: []benchmark.Service{
			{
				Host:  e.Host(),
				Name:  e.Name("web"),
				Image: "rtcbench-livekit-web:latest",
			},
		},
		Start: func(ctx context.Context, remote benchmark.Remote) error { return start(ctx, remote, e) },
		Stop:  func(ctx context.Context, remote benchmark.Remote) error { return stop(ctx, remote, e) },
		Artifacts: []benchmark.Artifact{
			{
				Host: e.Host(),
				Path: path.Join(e.Directory(), "livekit.log"),
				Name: "sfu.log",
			},
		},
		Assets: []benchmark.Asset{
			{
				Local:      filepath.Join(e.BinariesDir, "livekit-server"),
				Remote:     "/tmp/bench-bin/livekit-server",
				Hosts:      e.SFU,
				Executable: true,
			},
		},
	}
}
