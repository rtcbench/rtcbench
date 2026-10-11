package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

func TestCustomDeploymentRegistration(t *testing.T) {
	plugin := testDeploymentPlugin()
	dir := t.TempDir()
	m, err := New(dir, plugin)
	must(t, err)
	t.Cleanup(func() { m.Close(context.Background()) })
	m.LoadBuildContext = func(_ context.Context, id string) (map[string]string, error) {
		return map[string]string{id + "/Dockerfile": "FROM custom-image"}, nil
	}
	h := savedHost(t, m)
	var commands []string
	var archive string
	m.remote = func(_ context.Context, _ credential, command string, input io.Reader, _ io.Writer) error {
		commands = append(commands, command)
		if input != nil {
			data, err := io.ReadAll(input)
			archive = string(data)
			return err
		}
		return nil
	}
	d, err := m.start(Start{Plugin: "custom", HostID: h.ID, Address: "10.0.0.5"})
	must(t, err)
	ready := wait(t, m, d.ID)
	failIf(t, ready.Status != "ready", ready)
	failIf(t, len(commands) == 0 || commands[0] != "check-custom-host", commands)
	for _, want := range []string{"custom/Dockerfile", `"media"`} {
		if !strings.Contains(archive, want) {
			t.Errorf("build archive is missing %q", want)
		}
	}
	config, env, err := m.Bind(d.ID, []byte("spec:\n  pluginConfig:\n    old:\n      token: $env:UNAVAILABLE\n"))
	must(t, err)
	for _, value := range []string{"custom", "http://10.0.0.5:9123", "enabled: true"} {
		failIf(t, !strings.Contains(string(config), value), string(config))
	}
	failIf(t, strings.Contains(string(config), "UNAVAILABLE"), "binding retained the old plugin config")
	if got := env["PLUGIN_ID"]; got != "custom" {
		t.Errorf("PLUGIN_ID = %q, want custom", got)
	}
	target, err := m.TerminalTarget("deployments", d.ID, "")
	must(t, err)
	failIf(t, target.Service != "media", target.Service)
	var inventory struct{ Plugins []deploy.Plugin }
	must(t, json.Unmarshal(request(t, m, "GET", "/systems", nil).Body.Bytes(), &inventory))
	failIf(t, len(inventory.Plugins) != 1 || inventory.Plugins[0].ID != "custom", inventory)
	reloaded, err := New(dir, plugin)
	must(t, err)
	defer reloaded.Close(context.Background())
	failIf(t, reloaded.db.Deployments[d.ID].Ports["endpoint"] != 9123, "custom ports did not persist")
}

func TestPluginRegistration(t *testing.T) {
	plugin := testDeploymentPlugin()
	registered, err := New(t.TempDir(), plugin)
	must(t, err)
	defer registered.Close(context.Background())
	t.Run("isolated between managers", func(t *testing.T) {
		other, err := New(t.TempDir())
		must(t, err)
		defer other.Close(context.Background())
		_, err = other.start(Start{Plugin: "custom", Address: "192.0.2.5"})
		if err == nil {
			t.Fatal("unregistered plugin accepted")
		}
	})
	t.Run("duplicate ID", func(t *testing.T) {
		m, err := New(t.TempDir(), plugin, plugin)
		if err == nil {
			m.Close(context.Background())
			t.Fatal("duplicate plugin registration accepted")
		}
	})
}

func testDeploymentPlugin() deploy.Plugin {
	return deploy.Plugin{
		ID:              "custom",
		Name:            "Custom SFU",
		Ports:           []deploy.Port{{ID: "endpoint", Name: "Endpoint", Protocol: "TCP", Default: 9123}},
		TerminalService: "media",
		Bind: func(d deploy.Config) (map[string]any, map[string]string) {
			return map[string]any{
				"url":     fmt.Sprintf("http://%s:%d", d.Address, d.Ports["endpoint"]),
				"enabled": true,
			}, nil
		},
		Recipe: func(d deploy.Config) (map[string]deploy.Object, map[string][]byte) {
			return map[string]deploy.Object{"media": deploy.BuiltService(d, "true")}, nil
		},
		CheckHost: func(d deploy.Config, run func(string) (string, error)) error {
			_, err := run("check-custom-host")
			return err
		},
	}
}
