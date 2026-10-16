package bench

import (
	"fmt"
	"slices"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
	janusbench "github.com/rtcbench/rtcbench/plugin/janus/bench"
	jitsibench "github.com/rtcbench/rtcbench/plugin/jitsi/bench"
	livekitbench "github.com/rtcbench/rtcbench/plugin/livekit/bench"
	mediasoupbench "github.com/rtcbench/rtcbench/plugin/mediasoup/bench"
)

type JanusConfig = janusbench.Config
type JitsiConfig = jitsibench.Config
type LiveKitConfig = livekitbench.Config
type MediasoupConfig = mediasoupbench.Config

func (c Cluster) plugins(id string) []benchmark.Plugin {
	env := benchmark.Environment{
		ID:          id,
		SFU:         c.SFU,
		DataIPs:     c.DataIPs,
		BinariesDir: c.BinariesDir,
	}
	return []benchmark.Plugin{
		janusbench.New(env, c.JanusConfig),
		jitsibench.New(env, c.JitsiConfig),
		livekitbench.New(env, c.LiveKitConfig),
		mediasoupbench.New(env, c.MediasoupConfig),
	}
}

func (c Cluster) plugin(sfu, id string) (benchmark.Plugin, error) {
	for _, plugin := range c.plugins(id) {
		if plugin.ID == sfu {
			return plugin, nil
		}
	}
	return benchmark.Plugin{}, fmt.Errorf("unknown benchmark plugin %q", sfu)
}

func pluginIDs() []string {
	var ids []string
	for _, plugin := range (Cluster{}).plugins("") {
		ids = append(ids, plugin.ID)
	}
	slices.Sort(ids)
	return ids
}

func defaultPluginConfig() Cluster {
	return Cluster{
		JanusConfig:     janusbench.DefaultConfig(),
		JitsiConfig:     jitsibench.DefaultConfig(),
		LiveKitConfig:   livekitbench.DefaultConfig(),
		MediasoupConfig: mediasoupbench.DefaultConfig(),
	}
}
