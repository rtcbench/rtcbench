package bench

import (
	"fmt"
	"strconv"

	"github.com/goccy/go-yaml"
	"github.com/rtcbench/rtcbench/internal/core"
	"github.com/rtcbench/rtcbench/internal/launch"
)

func nativeConfig(c Cluster, cell Cell, sfuIP, host string, group RoomGroup, sender bool, dir string) ([]byte, error) {
	plugin, err := c.plugin(cell.SFU, "")
	if err != nil {
		return nil, err
	}
	file := "rtcbench-configs/bench-receiver.yml"
	if sender {
		file = "rtcbench-configs/bench-sender-load.yml"
	}
	data, err := assets.ReadFile(file)
	if err != nil {
		return nil, err
	}
	values := map[string]string{
		"PLUGIN_ID":        cell.SFU,
		"SERVER_IP":        sfuIP,
		"VIEWERS_PER_ROOM": strconv.Itoa(group.Users),
		"SENDERS_PER_ROOM": strconv.Itoa(group.Users),
		"TOTAL_ROOMS":      strconv.Itoa(group.Rooms),
		"IVF_DIR":          c.RemoteIVF,
		"LOG_DIR":          dir + "/logs",
		"STATS_JSONL_PATH": dir + "/stats",
		"JOIN_CONCURRENCY": strconv.Itoa(min(group.Users, 25)),
		"JOIN_SPACING":     "0s",
	}
	if sender && plugin.SenderJoinLimit > 0 {
		values["JOIN_CONCURRENCY"] = strconv.Itoa(plugin.SenderJoinLimit)
		values["JOIN_SPACING"] = plugin.SenderJoinSpacing
	}
	data, err = launch.RenderEnvYAML(data, func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		return nil, err
	}
	var config core.YAMLConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.Metadata == nil || config.Spec == nil || config.Spec.Conference == nil || config.Spec.Network == nil || config.Spec.Logging == nil {
		return nil, fmt.Errorf("incomplete benchmark template %s", file)
	}
	if !sender {
		config.Metadata.Name = pointer("bench-" + cell.SFU)
	}
	config.Spec.Conference.Name = pointer(fmt.Sprintf("room-%d", 1234+group.Offset))
	config.Spec.Metrics = &core.YAMLMetricsConfig{Port: pointer(0), StatsJSONLPath: pointer(dir + "/stats")}
	config.Spec.Logging.Console = false
	if ip := c.DataIPs[host]; ip != "" {
		config.Spec.Network.ClientIP = pointer(ip)
	}
	config.Spec.PluginConfig = map[string]any{cell.SFU: plugin.NativeConfig(sfuIP)}
	if _, err := config.IntoConfig(); err != nil {
		return nil, fmt.Errorf("benchmark config: %w", err)
	}
	return yaml.Marshal(config)
}

func pointer[T any](value T) *T { return &value }
