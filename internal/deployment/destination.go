package deployment

import (
	"errors"
	"fmt"

	"github.com/goccy/go-yaml"
	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

// Bind must run before environment/template expansion
func (m *Manager) Bind(id string, source []byte) ([]byte, map[string]string, error) {
	m.mu.Lock()
	d, ok := m.db.Deployments[id]
	m.mu.Unlock()
	if !ok || (d.Status != "ready" && d.Status != "configured") || d.Operation != "" {
		return nil, nil, errors.New("select a ready or externally configured SFU")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(source, &doc); err != nil {
		return nil, nil, err
	}
	if doc == nil {
		return nil, nil, errors.New("config is empty")
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
		doc["spec"] = spec
	}
	network, _ := spec["network"].(map[string]any)
	if network == nil {
		network = map[string]any{}
		spec["network"] = network
	}
	spec["plugin"] = d.Plugin
	delete(spec, "_plugin")
	network["serverIP"] = d.Address
	delete(network, "_serverIP")
	blocks, _ := spec["pluginConfig"].(map[string]any)
	config, _ := blocks[d.Plugin].(map[string]any)
	if config == nil {
		config = map[string]any{}
	}
	binding, env := m.plugins[d.Plugin].Bind(d.config())
	for key, value := range binding {
		delete(config, "_"+key)
		config[key] = value
	}
	// Unused plugin blocks may contain unavailable environment references
	spec["pluginConfig"] = map[string]any{d.Plugin: config}
	if env == nil {
		env = map[string]string{}
	}
	env["PLUGIN_ID"], env["SERVER_IP"] = d.Plugin, d.Address
	data, err := yaml.Marshal(doc)
	return data, env, err
}

func (m *Manager) Name(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.db.Deployments[id].Name
}

func settings(plugin deploy.Plugin, d deployment) map[string]string {
	values := map[string]string{"spec.plugin": d.Plugin, "spec.network.serverIP": d.Address}
	config, _ := plugin.Bind(d.config())
	for key, value := range config {
		values["spec.pluginConfig."+d.Plugin+"."+key] = fmt.Sprint(value)
	}
	return values
}
