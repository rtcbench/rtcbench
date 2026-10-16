package bench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rtcbench/rtcbench/internal/launch"
)

type recordingRemote struct {
	mu               sync.Mutex
	commands         []string
	files            map[string]string
	fail             error
	collectFail      error
	cancelledCleanup bool
}

func (r *recordingRemote) Run(ctx context.Context, host, command string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, host+": "+command)
	if strings.HasPrefix(command, "docker inspect --format '{{json .State}}' ") {
		return "{\"Running\":true}", nil
	}
	if strings.HasPrefix(command, "date +%s; tail") {
		var data strings.Builder
		fmt.Fprintf(&data, "%d\n==> /tmp/stats/streams.jsonl <==\n", time.Now().Unix())
		for i := 0; i < 8; i++ {
			fmt.Fprintf(&data, "{\"ts\":%d,\"viewer\":\"v%d\",\"fps\":30,\"bitrate_bps\":3500000}\n", time.Now().Unix(), i)
		}
		return data.String(), nil
	}
	if strings.HasPrefix(command, "now=$(date") {
		return browserStatsReply(16), nil
	}
	if strings.Contains(command, "stat -c %Y") {
		return fmt.Sprintf("%d\n%d\npages_sum,videoRecvBitrates_length,videoRecvBitrates_min,videoRecvFps_min\n1000000,1000000,3500000,30\n", time.Now().Unix(), time.Now().Unix()), nil
	}
	if (strings.HasPrefix(command, "find ") || strings.HasPrefix(command, "docker rm")) && ctx.Err() != nil {
		r.cancelledCleanup = true
	}
	if strings.HasPrefix(command, "uname") {
		return "", r.fail
	}
	if strings.HasPrefix(command, "find ") {
		return "raw stats\n", r.collectFail
	}
	if strings.HasPrefix(command, "docker logs") {
		return "container output\n", nil
	}
	return "", nil
}
func (r *recordingRemote) Write(ctx context.Context, host, path string, data []byte) error {
	if r.files == nil {
		r.files = map[string]string{}
	}
	r.files[host+path] = string(data)
	return nil
}

func TestExperimentCleansUpAfterMeasurementSetupFailure(t *testing.T) {
	req := testRequest()
	req.DryRun = false
	c, _ := ParseCluster(req.ClusterYAML)
	c.StartLeadSeconds = 0
	layout, _ := Layout(c, req.Mode, "livekit", 5)
	want := errors.New("host unreachable")
	remote := &recordingRemote{fail: want}
	e := experiment{
		request:   req,
		cluster:   c,
		remote:    remote,
		topology:  layout,
		cell:      req.Cells[0],
		dir:       t.TempDir(),
		remoteDir: "/dev/shm/rtcbench-bench/test-001",
		log:       io.Discard,
		progress:  func(string) error { return nil },
	}
	trial, err := e.run(context.Background(), 1)
	if !errors.Is(err, want) || trial.Passed || trial.Reason != "EXPERIMENT_ERROR" {
		t.Fatalf("%+v %v", trial, err)
	}
	removed := 0
	for _, command := range remote.commands {
		if strings.Contains(command, "docker rm -f") {
			removed++
		}
	}
	if removed != 2 {
		t.Fatalf("cleanup count %d: %v", removed, remote.commands)
	}
	stopped, collected := false, false
	for _, command := range remote.commands {
		if strings.Contains(command, "docker stop") {
			stopped = true
		}
		if strings.Contains(command, ": find ") {
			if !stopped {
				t.Error("collected while load containers were still running")
			}
			collected = true
		}
		if strings.Contains(command, "docker rm") && !collected {
			t.Error("removed containers before collecting evidence")
		}
	}
	for _, name := range []string{"container-0.jsonl", "container-0.log", "container-1.jsonl", "container-1.log"} {
		if data, err := os.ReadFile(filepath.Join(e.dir, name)); err != nil || len(data) == 0 {
			t.Fatalf("missing failure evidence %s: %v", name, err)
		}
	}
	if !strings.Contains(strings.Join(remote.commands, "\n"), "rm -rf -- '/dev/shm/rtcbench-bench/test-001'") {
		t.Fatal("trial tmpfs was not cleaned")
	}
}

func TestExperimentKeepsRemoteEvidenceWhenCollectionFails(t *testing.T) {
	req := testRequest()
	c, _ := ParseCluster(req.ClusterYAML)
	c.StartLeadSeconds = 0
	layout, _ := Layout(c, req.Mode, "livekit", 5)
	want := errors.New("disk unavailable")
	remote := &recordingRemote{fail: errors.New("cancelled during capture"), collectFail: want}
	e := experiment{
		request:   req,
		cluster:   c,
		remote:    remote,
		topology:  layout,
		cell:      req.Cells[0],
		dir:       t.TempDir(),
		remoteDir: "/dev/shm/rtcbench-bench/retain-001",
		log:       io.Discard,
		progress:  func(string) error { return nil },
	}
	_, err := e.run(context.Background(), 1)
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	for _, cmd := range remote.commands {
		if strings.Contains(cmd, "rm -rf") {
			t.Fatal("deleted uncollected evidence")
		}
	}
}

func TestNativeLoadUsesCPUSetAndExactRoomRemainder(t *testing.T) {
	req := testRequest()
	c, _ := ParseCluster(req.ClusterYAML)
	c.Cores = "0,2,4,6"
	layout, _ := Layout(c, req.Mode, "livekit", 21)
	remote := &recordingRemote{}
	e := experiment{
		request:   req,
		cluster:   c,
		remote:    remote,
		topology:  layout,
		cell:      req.Cells[0],
		dir:       t.TempDir(),
		remoteDir: "/dev/shm/rtcbench-bench/test-002",
	}
	for i, g := range layout.Groups {
		if err := e.native(context.Background(), layout.Workers[0], g, false, true, i); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range remote.commands {
		if strings.Contains(command, "docker run") && !strings.Contains(command, "--cpuset-cpus '0,2,4,6'") {
			t.Fatalf("missing affinity: %s", command)
		}
	}
	configs := strings.Join(mapValues(remote.files), "\n")
	if !strings.Contains(configs, "usersPerRoom: 20") || !strings.Contains(configs, "usersPerRoom: 1") || !strings.Contains(configs, "name: room-1235") {
		t.Fatal(configs)
	}
}

func TestCancellationStillCollectsAndStopsContainers(t *testing.T) {
	req := testRequest()
	c, _ := ParseCluster(req.ClusterYAML)
	c.StartLeadSeconds = 0
	layout, _ := Layout(c, req.Mode, "livekit", 1)
	remote := &recordingRemote{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := experiment{
		request:   req,
		cluster:   c,
		remote:    remote,
		topology:  layout,
		cell:      req.Cells[0],
		dir:       t.TempDir(),
		remoteDir: "/dev/shm/rtcbench-bench/cancel-001",
		log:       io.Discard,
		progress: func(phase string) error {
			if strings.HasPrefix(phase, "Warming") {
				cancel()
			}
			return nil
		},
	}
	_, err := e.run(ctx, 1)
	if !errors.Is(err, context.Canceled) || remote.cancelledCleanup {
		t.Fatalf("cleanup cancelled: %v %v", remote.cancelledCleanup, err)
	}
	if !strings.Contains(strings.Join(remote.commands, "\n"), "docker rm -f") {
		t.Fatal("containers leaked")
	}
}

func TestCleanupRefusesUnownedDirectories(t *testing.T) {
	for _, path := range []string{"/dev/shm", "/dev/shm/rtcbench-bench/../other", "/dev/shm/rtcbench-bench/"} {
		e := experiment{remote: forbiddenRemote{t}, remoteDir: path}
		if err := e.removeArtifacts(context.Background()); err == nil {
			t.Fatal(path)
		}
	}
}

func mapValues(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}

func TestNativeConfigsUseExistingConfigValidation(t *testing.T) {
	c, _ := ParseCluster(testCluster)
	c.LiveKitWS = 17880
	for _, sfu := range []string{"livekit", "janus", "mediasoup", "jitsi"} {
		for _, sender := range []bool{false, true} {
			data, err := nativeConfig(c, Cell{SFU: sfu, Client: "rtcbench"}, "192.0.2.1", "192.0.2.3", RoomGroup{Offset: 1, Rooms: 2, Users: 3}, sender, "/dev/shm/bench/native")
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := launch.LoadConfig(data, func(string) (string, bool) { return "", false })
			if err != nil {
				t.Fatalf("%s sender=%v: %v\n%s", sfu, sender, err, data)
			}
			if cfg.Spec.Conference.Name != "room-1235" || cfg.Spec.Conference.UsersPerRoom != 3 || cfg.Spec.Conference.TotalRooms != 2 || cfg.Spec.Metrics.Port != 0 {
				t.Fatalf("%+v", cfg.Spec)
			}
			if sfu == "livekit" && !strings.Contains(string(data), ":17880") {
				t.Fatal("ignored port override")
			}
		}
	}
}

func TestPublishersStartBeforeReceivers(t *testing.T) {
	for _, mode := range []string{"sender", "receiver"} {
		for _, client := range Clients {
			t.Run(mode+"/"+client, func(t *testing.T) {
				c, _ := ParseCluster(testCluster)
				c.StartLeadSeconds, c.WarmupSeconds = 0, 0
				c.Senders = append(c.Senders, "192.0.2.4")
				layout, err := Layout(c, mode, "janus", 2)
				if err != nil {
					t.Fatal(err)
				}
				r := &recordingRemote{}
				e := experiment{
					request:   Request{Mode: mode},
					cluster:   c,
					remote:    r,
					topology:  layout,
					cell:      Cell{"janus", client},
					dir:       t.TempDir(),
					remoteDir: "/dev/shm/rtcbench-bench/order",
					progress:  func(string) error { return nil },
				}
				if err := e.prepare(t.Context()); err != nil {
					t.Fatal(err)
				}
				var hosts []string
				for _, command := range r.commands {
					if strings.Contains(command, ": docker run ") {
						host, _, _ := strings.Cut(command, ": ")
						hosts = append(hosts, host)
					}
				}
				want := append([]string{layout.Observer}, layout.Workers...)
				if mode == "sender" {
					want = append(append([]string{}, layout.Workers...), layout.Observer)
				}
				if strings.Join(hosts, ",") != strings.Join(want, ",") {
					t.Fatalf("publishers must precede receivers: got %v, want %v", hosts, want)
				}
			})
		}
	}
}
