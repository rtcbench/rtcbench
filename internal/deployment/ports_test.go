package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rtcbench/rtcbench/internal/launch"
)

func TestDeploymentPorts(t *testing.T) {
	for id, urlKey := range map[string]string{
		"livekit":   "wsURL",
		"janus":     "serverRoot",
		"mediasoup": "serverURL",
		"jitsi":     "boshURL",
	} {
		t.Run(id, func(t *testing.T) {
			m := manager(t)
			ports, err := resolvePorts(testPlugins(), id, nil, true)
			must(t, err)
			for key := range ports {
				ports[key] += 1000
			}
			d, err := m.start(Start{
				Plugin:  id,
				Address: "10.20.30.40",
				APIKey:  "key",
				Secret:  "secret",
				Ports:   ports,
			})
			must(t, err)
			reloaded, err := New(filepath.Dir(m.path), plugins...)
			must(t, err)
			defer reloaded.Close(context.Background())
			saved := reloaded.db.Deployments[d.ID]
			failIf(t, !maps.Equal(saved.Ports, ports), "ports were not persisted")
			config, env, err := reloaded.Bind(d.ID, []byte(`apiVersion: rtcbench/v1
kind: VideoCallStressTest
metadata:
  name: ports
spec:
  conference:
    name: rtcbench
    totalRooms: 1
    usersPerRoom: 2
    joinPolicy:
      concurrency: 2
  logging:
    directory: logs
`))
			must(t, err)
			cfg, err := launch.LoadConfig(config, func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			})
			must(t, err)
			options := cfg.Spec.PluginConfig[id].(map[string]any)
			failIf(t, !strings.Contains(options[urlKey].(string), fmt.Sprintf("10.20.30.40:%d", ports["signaling"])), options)
			response := request(t, reloaded, "PUT", "/deployments/"+d.ID+"/ports", map[string]int{"signaling": 8443})
			failIf(t, response.Code != 204, response)
			if got := reloaded.db.Deployments[d.ID].Ports["signaling"]; got != 8443 {
				t.Errorf("updated signaling port = %d, want 8443", got)
			}
		})
	}
}

func TestPortValidation(t *testing.T) {
	for _, tc := range []struct {
		plugin, port string
		value        int
	}{
		{"livekit", "signaling", 0},
		{"livekit", "rtcTcp", -1},
		{"janus", "udpEnd", 65536},
		{"janus", "udpStart", 20001},
		{"janus", "rtcTcp", 7881},
		{"livekit", "signaling", 7881},
		{"mediasoup", "signaling", 44444},
		{"jitsi", "http", 443},
		{"jitsi", "signaling", 5280},
	} {
		t.Run(fmt.Sprintf("%s/%s=%d", tc.plugin, tc.port, tc.value), func(t *testing.T) {
			_, err := resolvePorts(testPlugins(), tc.plugin, map[string]int{tc.port: tc.value}, true)
			if err == nil {
				t.Fatal("invalid port accepted")
			}
		})
	}
	t.Run("external deployment can use an internal Jitsi port", func(t *testing.T) {
		_, err := resolvePorts(testPlugins(), "jitsi", map[string]int{"signaling": 8080}, false)
		must(t, err)
	})
}

func TestPortRedeployment(t *testing.T) {
	m := manager(t)
	h := savedHost(t, m)
	var uploaded string
	m.remote = func(_ context.Context, _ credential, _ string, in io.Reader, _ io.Writer) error {
		if in != nil {
			data, err := io.ReadAll(in)
			uploaded = string(data)
			return err
		}
		return nil
	}
	d, err := m.start(Start{Plugin: "livekit", Address: "10.0.0.2", HostID: h.ID})
	must(t, err)
	wait(t, m, d.ID)
	failIf(t, m.savePorts(d.ID, map[string]int{"signaling": 8880}) == nil, "changed running listeners")
	must(t, m.action(d.ID, "stop"))
	wait(t, m, d.ID)
	must(t, m.savePorts(d.ID, map[string]int{"signaling": 8880}))
	must(t, m.action(d.ID, "retry"))
	wait(t, m, d.ID)
	for _, want := range []string{"port: 8880", "localhost:8880"} {
		if !strings.Contains(uploaded, want) {
			t.Errorf("redeployment config is missing %q", want)
		}
	}
}

func TestInvalidPortJSON(t *testing.T) {
	m := manager(t)
	for _, value := range []any{1.5, "8880", nil} {
		t.Run(fmt.Sprintf("%T", value), func(t *testing.T) {
			response := request(t, m, "POST", "/deployments", object{
				"plugin": "janus", "address": "10.0.0.3",
				"ports": object{"signaling": value},
			})
			if response.Code < 400 {
				t.Fatalf("accepted invalid port %v: %s", value, response.Body)
			}
		})
	}
}

func TestDeploymentPortDefaults(t *testing.T) {
	m := manager(t)
	d, err := m.start(Start{Plugin: "livekit", Address: "10.0.0.2", APIKey: "key", Secret: "secret"})
	must(t, err)
	record := m.db.Deployments[d.ID]
	record.Ports = nil
	m.db.Deployments[d.ID] = record
	must(t, m.save())
	reloaded, err := New(filepath.Dir(m.path), plugins...)
	must(t, err)
	defer reloaded.Close(context.Background())
	failIf(t, reloaded.db.Deployments[d.ID].Ports["signaling"] != 7880, "missing ports lost defaults")
	var inventory struct{ Deployments []Deployment }
	must(t, json.Unmarshal(request(t, reloaded, "GET", "/systems", nil).Body.Bytes(), &inventory))
	if len(inventory.Deployments) != 1 {
		t.Fatalf("inventory has %d deployments, want 1", len(inventory.Deployments))
	}
	failIf(t, inventory.Deployments[0].Ports["rtcTcp"] != 7881, "API omitted resolved defaults")
}

func TestMediasoupWorkerPortPreflight(t *testing.T) {
	for _, tc := range []struct {
		media, signaling int
		valid            bool
	}{
		{44444, 44450, false},
		{65521, 4443, false},
		{65520, 4443, true},
		{44444, 44460, true},
	} {
		t.Run(fmt.Sprint(tc.media, "-", tc.signaling), func(t *testing.T) {
			m := manager(t)
			h := savedHost(t, m)
			var commands []string
			m.remote = func(_ context.Context, _ credential, cmd string, _ io.Reader, out io.Writer) error {
				commands = append(commands, cmd)
				if cmd == "getconf _NPROCESSORS_ONLN" {
					_, err := io.WriteString(out, "16\n")
					return err
				}
				return nil
			}
			d, err := m.start(Start{
				Plugin:  "mediasoup",
				HostID:  h.ID,
				Address: "10.0.0.2",
				Ports:   map[string]int{"media": tc.media, "signaling": tc.signaling},
			})
			must(t, err)
			result := wait(t, m, d.ID)
			if tc.valid {
				failIf(t, result.Status != "ready", result.Error)
			} else {
				failIf(t, result.Status != "failed", result)
				failIf(t, !strings.Contains(result.Error, "16 workers"), result.Error)
				if len(commands) != 1 {
					t.Errorf("ran commands after failed port preflight: %v", commands)
				}
			}
		})
	}
}
