package deployment

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rtcbench/rtcbench/internal/launch"
	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
	janus "github.com/rtcbench/rtcbench/plugin/janus/deployment"
	jitsi "github.com/rtcbench/rtcbench/plugin/jitsi/deployment"
	livekit "github.com/rtcbench/rtcbench/plugin/livekit/deployment"
	mediasoup "github.com/rtcbench/rtcbench/plugin/mediasoup/deployment"
	"golang.org/x/crypto/ssh"
)

var plugins = []deploy.Plugin{livekit.Definition, janus.Definition, mediasoup.Definition, jitsi.Definition}

func testPlugins() map[string]deploy.Plugin {
	registered := map[string]deploy.Plugin{}
	for _, plugin := range plugins {
		registered[plugin.ID] = plugin
	}
	return registered
}

func manager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(t.TempDir(), plugins...)
	must(t, err)
	m.LoadBuildContext = func(_ context.Context, plugin string) (map[string]string, error) {
		return testBuildContext(plugin)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m
}

func testBuildContext(plugin string) (map[string]string, error) {
	files := map[string]string{}
	root := os.DirFS("../../docker")
	err := fs.WalkDir(root, plugin, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root, name)
		files[name] = string(data)
		return err
	})
	return files, err
}

func savedHost(t *testing.T, m *Manager) Host {
	t.Helper()
	h, err := m.saveHost(credential{
		Host: Host{
			Name:        "Lab",
			Address:     "127.0.0.1",
			User:        "ubuntu",
			Auth:        "password",
			Fingerprint: "SHA256:" + strings.Repeat("a", 43),
		},
		Password: "private-password",
	}, "")
	must(t, err)
	return h
}

func request(t *testing.T, m *Manager, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	m.Register(mux)
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func wait(t *testing.T, m *Manager, id string) deployment {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		d := m.db.Deployments[id]
		m.mu.Unlock()
		if d.Operation == "" {
			return d
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("deployment did not finish")
	return deployment{}
}

func TestLifecycleAndPersistence(t *testing.T) {
	m := manager(t)
	h := savedHost(t, m)
	gate := make(chan struct{})
	entered := make(chan struct{})
	m.remote = func(ctx context.Context, h credential, cmd string, in io.Reader, out io.Writer) error {
		if strings.Contains(cmd, "compose version") {
			close(entered)
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if strings.Contains(cmd, "ps --all") {
			_, _ = io.WriteString(out, `{"Service":"livekit","State":"running","Health":"healthy"}`)
		}
		if strings.Contains(cmd, "logs --") {
			_, _ = io.WriteString(out, "private-password")
		}
		if in != nil {
			_, _ = io.Copy(io.Discard, in)
		}
		return nil
	}
	d, err := m.start(Start{HostID: h.ID, Plugin: "livekit", Address: "10.0.0.2"})
	must(t, err)
	<-entered
	_, err = m.start(Start{HostID: h.ID, Plugin: "janus", Address: "10.0.0.2"})
	failIf(t, err == nil, "concurrent deployment allowed")
	failIf(t, m.action(d.ID, "stop") == nil, "concurrent operation allowed")
	failIf(t, request(t, m, "DELETE", "/systems/instances/"+h.ID, nil).Code != 409, "removed occupied server")
	close(gate)
	ready := wait(t, m, d.ID)
	failIf(t, ready.Status != "ready" || ready.CheckedAt == nil, ready)
	snapshot := request(t, m, "GET", "/systems", nil).Body.String()
	failIf(t, strings.Contains(snapshot, "private-password") || strings.Contains(snapshot, ready.Secret), "credential leak")
	must(t, m.action(d.ID, "logs"))
	got := wait(t, m, d.ID)
	failIf(t, got.Logs != "[redacted]" || got.Status != "ready", got)
	m.jobs.Wait()
	reloaded, err := New(strings.TrimSuffix(m.path, string(os.PathSeparator)+"state.json"), plugins...)
	must(t, err)
	defer reloaded.Close(context.Background())
	failIf(t, reloaded.db.Deployments[d.ID].Status != "unknown" || reloaded.db.Hosts[h.ID].Password != "private-password", "restart recovery lost state")
	must(t, m.action(d.ID, "stop"))
	failIf(t, wait(t, m, d.ID).Status != "stopped", "stop failed")
	failIf(t, request(t, m, "DELETE", "/deployments/"+d.ID, nil).Code != 204 || request(t, m, "DELETE", "/systems/instances/"+h.ID, nil).Code != 204, "could not remove stopped resources")
}

func TestFailureRecoveryAndShutdown(t *testing.T) {
	m := manager(t)
	h := savedHost(t, m)
	m.remote = func(ctx context.Context, _ credential, _ string, _ io.Reader, out io.Writer) error {
		_, _ = io.WriteString(out, "private-password")
		return errors.New("build failed")
	}
	d, err := m.start(Start{HostID: h.ID, Plugin: "janus", Address: "10.0.0.2"})
	must(t, err)
	failed := wait(t, m, d.ID)
	failIf(t, failed.Status != "failed" || strings.Contains(failed.Logs, "private-password"), failed)
	m.remote = func(context.Context, credential, string, io.Reader, io.Writer) error {
		return errors.New("logs unavailable")
	}
	must(t, m.action(d.ID, "logs"))
	got := wait(t, m, d.ID)
	failIf(t, got.Error != failed.Error || got.Logs != failed.Logs, "logs erased the deployment failure")
	failIf(t, m.action(d.ID, "retry") == nil, "retry before cleanup")
	_, err = m.start(Start{HostID: h.ID, Plugin: "jitsi", Address: "10.0.0.2"})
	failIf(t, err == nil, "failed deployment did not reserve host")
	_, err = m.saveHost(credential{Host: h, Password: "replacement-password"}, h.ID)
	must(t, err)
	m.remote = func(ctx context.Context, _ credential, _ string, _ io.Reader, _ io.Writer) error { return nil }
	must(t, m.action(d.ID, "stop"))
	wait(t, m, d.ID)
	m.remote = func(ctx context.Context, _ credential, _ string, _ io.Reader, _ io.Writer) error {
		<-ctx.Done()
		return ctx.Err()
	}
	must(t, m.action(d.ID, "retry"))
	m.Close(context.Background())
	got = wait(t, m, d.ID)
	failIf(t, got.Operation != "" || got.Status != "failed", got)
	failIf(t, m.action(d.ID, "stop") == nil, "operation after shutdown")
}

func TestValidationAndRequestBoundaries(t *testing.T) {
	m := manager(t)
	h := savedHost(t, m)
	for _, ip := range []string{
		"",
		"host; touch /tmp/x",
		"0.0.0.0",
		"224.0.0.1",
		"::1",
	} {
		_, err := m.start(Start{HostID: h.ID, Plugin: "janus", Address: ip})
		failIf(t, err == nil, ip)
	}
	_, err := m.start(Start{HostID: h.ID, Plugin: "unsupported", Address: "10.0.0.2"})
	failIf(t, err == nil, "unsupported plugin")
	_, err = m.saveHost(credential{Host: h, Password: "secret"}, "")
	failIf(t, err == nil, "duplicate host")
	h.Fingerprint = ""
	_, err = m.saveHost(credential{Host: h, Password: "secret"}, "")
	failIf(t, err == nil, "missing host key allowed")
	failIf(t, request(t, m, "POST", "/deployments", object{"unknown": "field"}).Code != 400, "unknown field accepted")
	mux := http.NewServeMux()
	m.Register(mux)
	for _, contentType := range []string{"text/plain", "application/json"} {
		r := httptest.NewRequest("POST", "http://localhost/api/v1/deployments", strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		failIf(t, w.Code != 403, w.Code)
	}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(private)
	cred := credential{Host: Host{Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}}
	must(t, cred.checkKey("", nil, signer.PublicKey()))
	cred.Fingerprint = "SHA256:" + strings.Repeat("a", 43)
	failIf(t, cred.checkKey("", nil, signer.PublicKey()) == nil, "host-key mismatch accepted")
}

func TestRecipes(t *testing.T) {
	for _, p := range plugins {
		t.Run(p.ID, func(t *testing.T) {
			d := deployment{Deployment: Deployment{ID: "abcd", Plugin: p.ID, Address: "10.20.30.40"}, Secret: strings.Repeat("a", 64)}
			var err error
			d.Ports, err = resolvePorts(testPlugins(), p.ID, nil, false)
			must(t, err)
			contextFiles, err := testBuildContext(p.ID)
			must(t, err)
			archive, err := bundle(p, d, contextFiles)
			must(t, err)
			tr := tar.NewReader(bytes.NewReader(archive))
			files := map[string][]byte{}
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				must(t, err)
				failIf(t, hdr.Mode != 0600, "insecure bundle mode")
				files[hdr.Name], _ = io.ReadAll(tr)
			}
			failIf(t, len(files[p.ID+"/Dockerfile"]) == 0, "missing existing Dockerfile")
			var compose struct{ Services map[string]object }
			must(t, json.Unmarshal(files["compose.yaml"], &compose))
			count := 1
			if p.ID == "jitsi" {
				count = 4
			}
			failIf(t, len(compose.Services) != count, "missing services")
			for _, s := range compose.Services {
				failIf(t, s["network_mode"] != "host" || s["healthcheck"] == nil, "missing network or health check")
			}
			if p.ID == "livekit" {
				runtime := string(files["livekit-runtime.yaml"])
				failIf(t, !strings.Contains(runtime, d.Secret), "LiveKit secret missing")
				failIf(t, !strings.Contains(runtime, d.Address), "LiveKit address missing")
			}
			failIf(t, strings.Contains(string(files["compose.yaml"]), "172.2"), "Docker-only address escaped recipe")
			m := manager(t)
			d.Status = "ready"
			m.db.Deployments[d.ID] = d
			config, env, err := m.Bind(d.ID, []byte(`apiVersion: rtcbench/v1
kind: VideoCallStressTest
metadata:
  name: test
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
				v, ok := env[key]
				return v, ok
			})
			must(t, err)
			failIf(t, cfg.Spec.Plugin != p.ID, "incorrect plugin binding")
		})
	}
}

func TestContainerHealth(t *testing.T) {
	failIf(t, healthy(`[{"State":"running","Health":"healthy"}]`, 1) != nil || healthy(`{"State":"running","Health":"healthy"}`, 1) != nil, "valid compose output rejected")
	for _, state := range []string{
		"",
		`[]`,
		`{"State":"exited","Health":"healthy"}`,
		`{"State":"running","Health":"unhealthy"}`,
		`not json`,
	} {
		failIf(t, healthy(state, 1) == nil, state)
	}
}

func TestBoundedOutput(t *testing.T) {
	var output tail
	_, _ = output.Write(bytes.Repeat([]byte("x"), 100000))
	failIf(t, len(output.String()) != 64<<10, "unbounded output")
}

func TestProxyWriteOrigins(t *testing.T) {
	m := manager(t)
	mux := http.NewServeMux()
	m.Register(mux)
	for _, site := range []string{
		"same-origin",
		"cross-site",
		"same-site",
		"",
	} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/v1/deployments", strings.NewReader(`{"plugin":"janus","address":"10.0.0.2"}`))
		r.Header.Set("Origin", "https://bench.example")
		r.Header.Set("Sec-Fetch-Site", site)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		want := 403
		if site == "same-origin" {
			want = 201
		}
		failIf(t, w.Code != want, w.Code)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	failIf(t, err != nil, err)
}

func TestNetworksAndExternalDestinations(t *testing.T) {
	m := manager(t)
	n, err := m.saveNetwork(Network{Name: "VPC", CIDR: "10.20.0.2/24"})
	must(t, err)
	failIf(t, n.CIDR != "10.20.0.0/24", n)
	for _, address := range []string{
		"10.21.0.1",
		"10.20.0.0",
		"10.20.0.255",
		"::1",
	} {
		failIf(t, m.checkAddress(Interface{n.ID, address}) == nil, address)
	}
	h := savedHost(t, m)
	h.Networks = []Interface{{n.ID, "10.20.0.10"}}
	_, err = m.saveHost(credential{Host: h}, h.ID)
	must(t, err)
	failIf(t, request(t, m, "DELETE", "/systems/networks/"+n.ID, nil).Code != 409, "deleted an instance network")
	failIf(t, m.db.Hosts[h.ID].Password != "private-password", "credential lost on network edit")
	_, err = m.start(Start{
		HostID:    h.ID,
		NetworkID: n.ID,
		Address:   "10.21.0.11",
		Plugin:    "janus",
	})
	failIf(t, err == nil, "out-of-network traffic IP accepted")
	m.remote = func(context.Context, credential, string, io.Reader, io.Writer) error {
		t.Error("external SFU used SSH")
		return nil
	}
	d, err := m.start(Start{
		Plugin:    "livekit",
		NetworkID: n.ID,
		Address:   "10.20.0.11",
		APIKey:    "key",
		Secret:    "private-token",
	})
	must(t, err)
	failIf(t, d.Status != "configured" || m.action(d.ID, "stop") == nil, "external SFU lifecycle")
	config, env, err := m.Bind(d.ID, []byte(`spec:
  plugin: janus
  conference:
    usersPerRoom: 12
  network:
    serverIP: 192.0.2.1
  pluginConfig:
    janus:
      token: $env:OLD_TOKEN
    livekit:
      _wsURL: old
      subscribe: false
`))
	must(t, err)
	for _, want := range []string{
		"10.20.0.11",
		"usersPerRoom: 12",
		"subscribe: false",
		"$env:SFU_API_SECRET",
	} {
		failIf(t, !strings.Contains(string(config), want), string(config))
	}
	failIf(t, env["SFU_API_SECRET"] != "private-token", "secret binding missing")
	for _, unwanted := range []string{"private-token", "OLD_TOKEN", "_wsURL"} {
		failIf(t, strings.Contains(string(config), unwanted), "incorrect binding: "+unwanted)
	}
	body := request(t, m, "GET", "/systems", nil).Body.String()
	failIf(t, strings.Contains(body, "private-token") || strings.Contains(body, "private-password"), "secret returned by API")

	reloaded, err := New(filepath.Dir(m.path), plugins...)
	must(t, err)
	defer reloaded.Close(context.Background())
	failIf(t, reloaded.db.Deployments[d.ID].Status != "configured" || len(reloaded.db.Networks) != 1, "external/network persistence")
	failIf(t, request(t, m, "DELETE", "/deployments/"+d.ID, nil).Code != 204, "cannot remove external SFU")
	failIf(t, request(t, m, "DELETE", "/systems/instances/"+h.ID, nil).Code != 204, "cannot remove instance")
	failIf(t, request(t, m, "DELETE", "/systems/networks/"+n.ID, nil).Code != 204, "cannot remove unused network")
}

func failIf(t *testing.T, condition bool, detail any) {
	t.Helper()
	if condition {
		t.Fatal(detail)
	}
}

func TestSharedSSHKeys(t *testing.T) {
	m := manager(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "test", []byte("private-passphrase"))
	must(t, err)
	path := filepath.Join(t.TempDir(), "key")
	must(t, os.WriteFile(path, pem.EncodeToMemory(block), 0600))
	h := savedHost(t, m)
	h.Auth = "file"
	_, err = m.saveHost(credential{Host: h}, h.ID)
	failIf(t, err == nil, "key authentication without a saved key accepted")
	k, err := m.saveKey(storedKey{SSHKey: SSHKey{Name: "Shared key", Path: path}, Passphrase: "private-passphrase"}, "")
	must(t, err)
	h.Auth, h.KeyID = "file", k.ID
	_, err = m.saveHost(credential{Host: h}, h.ID)
	must(t, err)
	reloaded, err := New(filepath.Dir(m.path), plugins...)
	must(t, err)
	defer reloaded.Close(context.Background())
	h = reloaded.db.Hosts[h.ID].Host
	failIf(t, h.KeyID != k.ID || len(reloaded.db.Keys) != 1 || reloaded.db.Hosts[h.ID].Password != "", "saved key or authentication lost on restart")
	k.Name = "Renamed key"
	response := request(t, reloaded, "PUT", "/systems/keys/"+k.ID, k)
	failIf(t, response.Code != 200 || strings.Contains(response.Body.String(), "passphrase"), response.Body.String())
	_, err = reloaded.hostCredential(reloaded.db.Hosts[h.ID]).authentication()
	must(t, err)
	body := request(t, reloaded, "GET", "/systems", nil).Body.String()
	failIf(t, strings.Contains(body, "private-passphrase") || strings.Contains(body, "PRIVATE KEY"), "key leaked")
	failIf(t, request(t, reloaded, "DELETE", "/systems/keys/"+k.ID, nil).Code != 409, "deleted used key")
	_, err = reloaded.saveHost(credential{Host: h}, h.ID)
	must(t, err)
	h.KeyID = "missing"
	_, err = reloaded.saveHost(credential{Host: h}, h.ID)
	failIf(t, err == nil, "missing key accepted")
	failIf(t, request(t, reloaded, "DELETE", "/systems/instances/"+h.ID, nil).Code != 204, "remove instance")
	failIf(t, request(t, reloaded, "DELETE", "/systems/keys/"+k.ID, nil).Code != 204, "remove unused key")
	failIf(t, request(t, reloaded, "POST", "/systems/keys", storedKey{SSHKey: SSHKey{Name: "Missing", Path: "relative"}}).Code != 400, "invalid key accepted")
	failIf(t, request(t, reloaded, "POST", "/systems/instances", object{"auth": "key", "privateKey": "old format"}).Code != 400, "inline key accepted")
}

func TestFailedWritesPreserveState(t *testing.T) {
	m := manager(t)
	h := savedHost(t, m)
	n, err := m.saveNetwork(Network{Name: "Lab", CIDR: "10.0.0.0/24"})
	must(t, err)
	d, err := m.start(Start{Plugin: "janus", Address: "10.0.0.1"})
	must(t, err)
	m.db.Keys["unused"] = storedKey{SSHKey: SSHKey{ID: "unused", Name: "Unused"}}
	must(t, m.save())
	before, err := os.ReadFile(m.path)
	must(t, err)
	m.path = filepath.Join(m.path, "unwritable")
	h.Name = "Edited"
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"PUT", "/systems/instances/" + h.ID, h},
		{"POST", "/systems/networks", Network{Name: "New", CIDR: "10.1.0.0/24"}},
		{"POST", "/deployments", Start{Plugin: "janus", Address: "10.0.0.2"}},
		{"PUT", "/deployments/" + d.ID + "/ports", map[string]int{"signaling": 9088}},
		{"DELETE", "/systems/instances/" + h.ID, nil},
		{"DELETE", "/systems/networks/" + n.ID, nil},
		{"DELETE", "/systems/keys/unused", nil},
		{"DELETE", "/deployments/" + d.ID, nil},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			failIf(t, request(t, m, tc.method, tc.path, tc.body).Code < 400, "failed write reported success")
			after, err := json.Marshal(m.db)
			must(t, err)
			failIf(t, !bytes.Equal(before, after), "failed write changed in-memory state")
		})
	}
}
