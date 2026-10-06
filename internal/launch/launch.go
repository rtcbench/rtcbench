package launch

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/rtcbench/rtcbench"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/plugin/janus"
	"github.com/rtcbench/rtcbench/plugin/jitsi"
	"github.com/rtcbench/rtcbench/plugin/livekit"
	"github.com/rtcbench/rtcbench/plugin/mediasoup"
)

func LoadConfig(data []byte, lookup LookupFunc) (*rtcbench.Config, error) {
	data, err := RenderEnvYAML(data, lookup)
	if err != nil {
		return nil, err
	}
	var yamlConfig rtcbench.YAMLConfig
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return nil, err
	}
	return yamlConfig.IntoConfig()
}

func Logs(lc rtcbench.LoggingConfig) (*log.Registry, error) {
	levels := make(map[string]log.Level, len(lc.Streams))
	for _, s := range lc.Streams {
		lvl, err := log.ParseLevel(s.Level)
		if err != nil {
			return nil, fmt.Errorf("stream %q: %w", s.Name, err)
		}
		levels[s.Name] = lvl
	}

	var handlers []log.Handler
	if lc.Console {
		consoleLvl, err := log.ParseLevel(lc.ConsoleLevel)
		if err != nil {
			return nil, fmt.Errorf("consoleLevel: %w", err)
		}
		handlers = append(handlers, log.NewConsoleHandler(lc.Color, consoleLvl))
	}
	if lc.Directory != "" {
		fileLvl, err := log.ParseLevel(lc.FileLevel)
		if err != nil {
			return nil, fmt.Errorf("fileLevel: %w", err)
		}
		fh, err := log.NewFileHandler(lc.Directory, fileLvl, lc.Combined)
		if err != nil {
			return nil, err
		}
		handlers = append(handlers, fh)
	}

	return log.NewRegistry(handlers, levels), nil
}

func RegisterPlugins(client *rtcbench.Client) {
	client.RegisterPlugin(jitsi.PluginID, jitsi.NewPlugin)
	client.RegisterPlugin(janus.PluginID, janus.NewPlugin)
	client.RegisterPlugin(livekit.PluginID, livekit.NewPlugin)
	client.RegisterPlugin(mediasoup.PluginID, mediasoup.NewPlugin)
}
