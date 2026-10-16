package bench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

type recordingRemote struct {
	commands []string
	files    map[string]string
}

func (r *recordingRemote) Run(ctx context.Context, host, command string) (string, error) {
	r.commands = append(r.commands, host+": "+command)
	return "", ctx.Err()
}

func (r *recordingRemote) Write(ctx context.Context, host, name string, data []byte) error {
	if r.files == nil {
		r.files = map[string]string{}
	}
	r.files[host+name] = string(data)
	return ctx.Err()
}

func TestProvisioningRoutesUseExactPeersAndOwnedRules(t *testing.T) {
	e := benchmark.Environment{
		ID:  "routing-001",
		SFU: []string{"192.0.2.10"},
		DataIPs: map[string]string{
			"192.0.2.10":    "10.2.96.10",
			"198.51.100.20": "10.3.17.20",
		},
	}
	r := &recordingRemote{}
	if err := routes(t.Context(), r, e, true); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(r.commands, "\n")
	for _, expected := range []string{"10.2.96.10", "10.3.17.20", "--comment 'rtcbench-routing-001'", "-C POSTROUTING"} {
		if !strings.Contains(commands, expected) {
			t.Errorf("missing %q in %s", expected, commands)
		}
	}
	if strings.Contains(commands, "/24") {
		t.Fatal("assumed a network mask")
	}
	r.commands = nil
	if err := routes(t.Context(), r, e, false); err != nil {
		t.Fatal(err)
	}
	for _, command := range r.commands {
		if !strings.Contains(command, "--comment 'rtcbench-routing-001'") || !strings.Contains(command, " -D ") {
			t.Fatal("unowned cleanup", command)
		}
	}
}

func TestLiveKitAllowsGracefulShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote shutdown uses Linux procfs")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "livekit.yml")
	ready := filepath.Join(dir, "ready")
	if err := os.WriteFile(config, []byte("trap 'sleep 6; exit 0' TERM\nprintf ready >"+benchmark.Quote(ready)+"\nwhile :; do sleep 0.1; done\n"), 0600); err != nil {
		t.Fatal(err)
	}
	process := exec.Command("sh", config)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown fixture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	exe, err := os.Readlink("/proc/" + strconv.Itoa(process.Process.Pid) + "/exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "livekit.pid"), []byte(strconv.Itoa(process.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	r := &recordingRemote{}
	e := benchmark.Environment{SFU: []string{"server"}, ID: "graceful"}
	if err := New(e, DefaultConfig()).Stop(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	for _, recorded := range r.commands {
		if !strings.Contains(recorded, "LiveKit did not stop") {
			continue
		}
		_, command, _ := strings.Cut(recorded, ": ")
		command = strings.ReplaceAll(command, e.Directory(), dir)
		command = strings.ReplaceAll(command, "/tmp/bench-bin/livekit-server", exe)
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput(); err != nil {
			t.Fatalf("graceful shutdown: %v: %s", err, output)
		}
		if _, err := os.Stat(filepath.Join(dir, "livekit.pid")); !os.IsNotExist(err) {
			t.Fatalf("PID file remains after shutdown: %v", err)
		}
		return
	}
	t.Fatal("missing LiveKit shutdown command")
}
