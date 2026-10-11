package deployment

import (
	"fmt"
	"regexp"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

var pluginID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func registerPlugins(definitions []deploy.Plugin) (map[string]deploy.Plugin, error) {
	plugins := make(map[string]deploy.Plugin, len(definitions))
	for _, plugin := range definitions {
		if !pluginID.MatchString(plugin.ID) || plugin.Bind == nil || plugin.Recipe == nil {
			return nil, fmt.Errorf("invalid deployment plugin definition %q", plugin.ID)
		}
		if _, exists := plugins[plugin.ID]; exists {
			return nil, fmt.Errorf("duplicate deployment plugin %q", plugin.ID)
		}
		if _, err := plugin.ResolvePorts(nil, true); err != nil {
			return nil, err
		}
		plugins[plugin.ID] = plugin
	}
	return plugins, nil
}
