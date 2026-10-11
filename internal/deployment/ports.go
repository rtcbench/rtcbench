package deployment

import (
	"errors"
	"fmt"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

func resolvePorts(plugins map[string]deploy.Plugin, id string, overrides map[string]int, managed bool) (map[string]int, error) {
	plugin, ok := plugins[id]
	if !ok {
		return nil, fmt.Errorf("deployment plugin %q is not registered", id)
	}
	return plugin.ResolvePorts(overrides, managed)
}

func (m *Manager) savePorts(id string, ports map[string]int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.db.Deployments[id]
	if !ok {
		return errors.New("deployment not found")
	}
	if d.Operation != "" || (d.HostID != "" && d.Status != "stopped") {
		return errors.New("stop the deployment before changing ports, then deploy again")
	}
	var err error
	d.Ports, err = resolvePorts(m.plugins, d.Plugin, ports, d.HostID != "")
	if err != nil {
		return err
	}
	return saveRecord(m, m.db.Deployments, id, d)
}
