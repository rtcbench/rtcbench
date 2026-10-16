package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rtcbench/rtcbench/internal/api"
	"github.com/rtcbench/rtcbench/internal/core"
)

const customConfig = `apiVersion: rtcbench/v1
kind: VideoCallStressTest
metadata:
  name: custom
spec:
  plugin: custom
  conference:
    name: room
    totalRooms: 1
    usersPerRoom: 1
    joinPolicy:
      concurrency: 1
  network:
    serverIP: 127.0.0.1
    clientIP: 127.0.0.1
  logging:
    console: false
    directory: ignored
`

type testPlugin struct {
	joined chan struct{}
	closed chan struct{}
}

func (p *testPlugin) Setup(context.Context, core.PluginEnv) error { return nil }

func (p *testPlugin) Shutdown(context.Context) error { close(p.closed); return nil }

func (p *testPlugin) NewParticipant(context.Context, *core.UserConfig) (core.Participant, error) {
	return p, nil
}

func (p *testPlugin) JoinRoom(context.Context, *core.JoinRequest) error { close(p.joined); return nil }

func (p *testPlugin) LeaveRoom(context.Context, *core.LeaveRequest) error { return nil }

func (p *testPlugin) Close() error { return nil }

func waitFor(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("plugin lifecycle timed out")
	}
}

func TestRunCustomPlugin(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bench.yml")
	config := strings.Replace(customConfig, "directory: ignored", "directory: "+filepath.ToSlash(t.TempDir()), 1)
	if err := os.WriteFile(file, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		plugin := &testPlugin{joined: make(chan struct{}), closed: make(chan struct{})}
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, []string{file}, Options{Register: func(c *core.Client) {
				c.RegisterPlugin("custom", func() core.Plugin { return plugin })
			}})
		}()
		select {
		case <-plugin.joined:
		case err := <-done:
			t.Fatalf("Run returned before joining: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("plugin did not join")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not shut down")
		}
		waitFor(t, plugin.closed)
	}
	if err := Run(t.Context(), []string{file}, Options{}); !errors.Is(err, core.ErrUnknownPlugin) {
		t.Fatalf("unregistered plugin: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := Run(ctx, []string{"-start-at", "4102444800", file}, Options{Register: func(*core.Client) { cancel() }}); err != nil {
		t.Fatalf("cancel scheduled run: %v", err)
	}
}

func TestRunArguments(t *testing.T) {
	for _, args := range [][]string{{"-bad"}, {"one.yml", "two.yml"}, {"missing.yml"}} {
		if err := Run(t.Context(), args, Options{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for range 2 {
		if err := Run(t.Context(), []string{"-version"}, Options{Version: "custom"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Run(t.Context(), []string{"-help"}, Options{}); err != nil {
		t.Fatal(err)
	}
}

type failingScenario struct{ err error }

func (s failingScenario) Run(context.Context, core.ScenarioEnv) error { return s.err }

type cancelOnWait struct {
	context.Context
	cancel context.CancelFunc
}

func (ctx cancelOnWait) Done() <-chan struct{} {
	ctx.cancel()
	return ctx.Context.Done()
}

func TestRunWaitsAfterNonFatalScenarioError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bench.yml")
	config := strings.Replace(customConfig, "directory: ignored", "directory: "+filepath.ToSlash(t.TempDir()), 1)
	if err := os.WriteFile(file, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, scenarioErr := range []error{
		errors.New("one participant failed to join"),
		core.ErrUnknownPlugin,
		core.ErrUnknownScenario,
		core.ErrUserExists,
	} {
		t.Run(scenarioErr.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := runConfig(cancelOnWait{ctx, cancel}, file, 0, func(c *core.Client) {
				c.RegisterScenario(core.DefaultScenarioID, func() core.Scenario { return failingScenario{scenarioErr} })
			})
			fatal := errors.Is(scenarioErr, core.ErrUnknownPlugin) || errors.Is(scenarioErr, core.ErrUnknownScenario) || errors.Is(scenarioErr, core.ErrUserExists)
			if fatal {
				if !errors.Is(err, scenarioErr) {
					t.Fatal("lost fatal scenario error", err)
				}
			} else if err != nil || ctx.Err() == nil {
				t.Fatal("non-fatal run did not wait for cancellation", err)
			}
		})
	}
}

func TestAPIPlugin(t *testing.T) {
	dir := t.TempDir()
	srv, err := api.New(dir, "rtcbench.yml", "custom", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Shutdown(context.Background()) })
	created := make(chan *testPlugin, 2)
	srv.Register = func(c *core.Client) {
		c.RegisterPlugin("custom", func() core.Plugin {
			p := &testPlugin{joined: make(chan struct{}), closed: make(chan struct{})}
			created <- p
			return p
		})
	}
	srv.Examples = os.DirFS("../../config-examples")
	h := srv.Handler()
	for _, manifests := range []fs.FS{nil, os.DirFS("../..")} {
		srv.Plugins = manifests
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/plugins", nil))
		var listed []any
		if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}
		if (manifests == nil && len(listed) != 0) || (manifests != nil && len(listed) != 4) {
			t.Fatal("plugin manifests do not match supplied set", listed)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/examples", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "1-camera-5-viewers.yml") {
		t.Fatalf("examples: %d %s", rec.Code, rec.Body)
	}
	var plugins []*testPlugin
	for range 2 {
		body, _ := json.Marshal(api.StartRequest{Name: "custom", Config: customConfig})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/runs", strings.NewReader(string(body))))
		if rec.Code != 201 {
			t.Fatalf("start: %d %s", rec.Code, rec.Body)
		}
		select {
		case plugin := <-created:
			plugins = append(plugins, plugin)
			waitFor(t, plugin.joined)
		case <-time.After(10 * time.Second):
			t.Fatal("API did not create custom plugin")
		}
	}
	srv.Shutdown(t.Context())
	for _, plugin := range plugins {
		waitFor(t, plugin.closed)
	}
}

func TestRunDesktopServesUntilTheWindowCloses(t *testing.T) {
	open := openWindow
	t.Cleanup(func() { openWindow = open })
	config := t.TempDir()
	for _, name := range []string{"AppData", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(name, config)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var seen string
	openWindow = func(_ context.Context, url, _ string) error {
		if !strings.HasPrefix(url, "http://127.0.0.1:") || strings.HasSuffix(url, ":8080") {
			return fmt.Errorf("unexpected url %s", url)
		}
		res, err := http.Get(url + "/api/v1/version")
		if err != nil {
			return err
		}
		defer res.Body.Close()
		var body struct{ Version string }
		err = json.NewDecoder(res.Body).Decode(&body)
		seen = body.Version
		return err
	}
	if err := Run(ctx, nil, Options{Version: "desktop-test"}); err != nil {
		t.Fatal(err)
	}
	if seen != "desktop-test" {
		t.Fatalf("window saw version %q", seen)
	}
	if _, err := os.Stat(filepath.Join(config, "RTCBench", "data", "configs")); err != nil {
		t.Fatal("desktop data is not in the user config directory:", err)
	}

	openWindow = func(context.Context, string, string) error { return errors.New("no display") }
	if err := Run(ctx, nil, Options{}); err == nil || !strings.Contains(err.Error(), "no display") {
		t.Fatalf("Run returned %v, want the window error", err)
	}
}
