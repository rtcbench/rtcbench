package bench

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type startingBrowsers struct {
	recordingRemote
	pages   []int
	streams []int
	calls   int
	state   string
}

func (r *startingBrowsers) Run(ctx context.Context, host, command string) (string, error) {
	if strings.HasPrefix(command, "docker inspect --format '{{json .State}}' ") {
		if r.state != "" {
			return r.state + "\n", nil
		}
		return "{\"Running\":true}\n", nil
	}
	if strings.HasPrefix(command, "now=$(date") {
		streams := r.pages[min(max(r.calls-1, 0), len(r.pages)-1)]
		if len(r.streams) > 0 {
			streams = r.streams[min(max(r.calls-1, 0), len(r.streams)-1)]
		}
		return browserStatsReply(streams), nil
	}
	if strings.Contains(command, "stat -c %Y") {
		r.calls++
		pages := r.pages[min(r.calls-1, len(r.pages)-1)]
		streams := pages
		if len(r.streams) > 0 {
			streams = r.streams[min(r.calls-1, len(r.streams)-1)]
		}
		return fmt.Sprintf("%d\n%d\npages_sum,videoRecvBitrates_length,videoRecvBitrates_min,videoRecvFps_min\n%d,%d,0,0\n", time.Now().Unix(), time.Now().Unix(), pages, streams), nil
	}
	return r.recordingRemote.Run(ctx, host, command)
}

func TestBrowserWarmupWaitsForAllPages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _ := ParseCluster(testCluster)
		c.StartLeadSeconds, c.WarmupSeconds = 0, 3
		layout, _ := Layout(c, "receiver", "livekit", 16)
		remote := &startingBrowsers{pages: []int{8, 16}}
		start := time.Now()
		var warmup time.Time
		e := experiment{
			request:   Request{Mode: "receiver"},
			cluster:   c,
			remote:    remote,
			topology:  layout,
			cell:      Cell{"livekit", "chromium"},
			dir:       t.TempDir(),
			remoteDir: "/dev/shm/rtcbench-bench/startup",
			progress: func(phase string) error {
				if strings.HasPrefix(phase, "Warming") {
					warmup = time.Now()
				}
				return nil
			},
		}
		if err := e.prepare(t.Context()); err != nil {
			t.Fatal(err)
		}
		if remote.calls < 2 || warmup.Sub(start) < 5*time.Second || time.Since(warmup) != 3*time.Second {
			t.Fatalf("measurement raced browser startup: calls=%d warmup=%v elapsed=%v", remote.calls, warmup.Sub(start), time.Since(warmup))
		}
	})
}

func TestBrowserStartupFailureIsAFailedTrial(t *testing.T) {
	for _, tc := range []struct{ name, state, reason string }{
		{"timeout", "", "STARTUP_TIMEOUT"},
		{"oom", "{\"Running\":false,\"OOMKilled\":true}", "OOM"},
		{"child oom", "{\"Running\":true,\"OOMKilled\":true}", "OOM"},
		{"exit", "{\"Running\":false,\"ExitCode\":1}", "CONTAINER_EXITED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, _ := ParseCluster(testCluster)
				c.StartLeadSeconds, c.WarmupSeconds = 0, 0
				c.StartupSeconds = 10
				layout, _ := Layout(c, "receiver", "livekit", 16)
				remote := &startingBrowsers{pages: []int{8}, state: tc.state}
				e := experiment{
					request:   Request{Mode: "receiver"},
					cluster:   c,
					remote:    remote,
					topology:  layout,
					cell:      Cell{"livekit", "chromium"},
					dir:       t.TempDir(),
					remoteDir: "/dev/shm/rtcbench-bench/startup",
					progress:  func(string) error { return nil },
				}
				trial, err := e.run(t.Context(), 1)
				if err != nil || trial.Passed || trial.Reason != tc.reason || len(trial.Samples) != 0 {
					t.Fatalf("startup failure misclassified: %+v %v", trial, err)
				}
			})
		})
	}
}

func TestBrowserStartupCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _ := ParseCluster(testCluster)
		remote := &startingBrowsers{pages: []int{0}}
		e := experiment{
			cluster:    c,
			remote:     remote,
			topology:   Topology{Load: 16},
			containers: []container{{host: "client", name: "browser", stats: "/tmp/stats", browser: true}},
		}
		ctx, cancel := context.WithCancel(t.Context())
		go func() { time.Sleep(time.Second); cancel() }()
		if err := e.waitForBrowsers(ctx); err != context.Canceled {
			t.Fatal(err)
		}
	})
}

type startupReply struct {
	recordingRemote
	reply string
}

func (r *startupReply) Run(_ context.Context, _ string, command string) (string, error) {
	state, stats, _ := strings.Cut(r.reply, "\n")
	if strings.HasPrefix(command, "docker inspect") {
		return state, nil
	}
	return stats, nil
}

func TestBrowserStartupStats(t *testing.T) {
	c, _ := ParseCluster("")
	for _, tc := range []struct {
		name, stats    string
		ready, invalid bool
	}{
		{"file absent", "", false, false},
		{"file being created", "100\n100\n", false, false},
		{"header only", "100\n100\npages_sum\n", false, false},
		{"still launching", "100\n100\npages_sum\n8\n", false, false},
		{"all launched", "100\n100\npages_sum\n16\n", true, false},
		{"stale", "100\n10\npages_sum\n16\n", false, false},
		{"invalid", "100\n100\npages_sum\nNaN\n", false, true},
		{"missing metric", "100\n100\nother\n16\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := experiment{
				cluster:  c,
				topology: Topology{Load: 16},
				remote:   &startupReply{reply: "{\"Running\":true}\n" + tc.stats},
			}
			ready, err := e.browserStarted(t.Context(), container{host: "client", name: "browser", stats: "/tmp/stats"})
			if ready != tc.ready || (err != nil) != tc.invalid {
				t.Fatalf("ready=%t err=%v", ready, err)
			}
		})
	}
}

func TestMeasurementRejectsSurvivingContainerAfterOOM(t *testing.T) {
	c, _ := ParseCluster("")
	e := experiment{
		cluster:    c,
		request:    Request{Mode: "receiver"},
		topology:   Topology{Load: 16},
		remote:     &startupReply{reply: "{\"Running\":true,\"OOMKilled\":true}"},
		containers: []container{{host: "client", name: "browser", stats: "/tmp/stats", browser: true}},
	}
	quality, err := e.measure(t.Context(), "client")
	if err != nil || quality.Passed || quality.Reason != "OOM" {
		t.Fatalf("%+v %v", quality, err)
	}
}

type oomWithoutStats struct{ recordingRemote }

func (r *oomWithoutStats) Run(_ context.Context, _ string, command string) (string, error) {
	if strings.Contains(command, "stat -c %Y") {
		return "", fmt.Errorf("stats unavailable during OOM")
	}
	return "{\"Running\":true,\"OOMKilled\":true}\n", nil
}
func TestBrowserOOMDoesNotReadStats(t *testing.T) {
	e := experiment{remote: &oomWithoutStats{}}
	_, err := e.browserStarted(t.Context(), container{host: "client", name: "browser", stats: "/tmp/stats"})
	failure, ok := err.(*startupFailure)
	if !ok || failure.reason != "OOM" {
		t.Fatalf("OOM should be handled without reading stats: %v", err)
	}
}

type joiningNative struct {
	recordingRemote
	counts []int
	reads  int
}

func (r *joiningNative) Run(ctx context.Context, host, command string) (string, error) {
	if strings.HasPrefix(command, "date +%s; tail") {
		count := r.counts[min(r.reads, len(r.counts)-1)]
		r.reads++
		var data strings.Builder
		fmt.Fprintf(&data, "%d\n==> /tmp/stats/streams.jsonl <==\n", time.Now().Unix())
		for i := 0; i < count; i++ {
			fmt.Fprintf(&data, "{\"ts\":%d,\"viewer\":\"v%d\",\"fps\":0,\"bitrate_bps\":0}\n", time.Now().Unix(), i)
		}
		return data.String(), nil
	}
	return r.recordingRemote.Run(ctx, host, command)
}
func TestNativeWarmupWaitsForEveryStreamWithoutRequiringQuality(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _ := ParseCluster(testCluster)
		c.StartLeadSeconds, c.WarmupSeconds = 0, 3
		layout, _ := Layout(c, "receiver", "livekit", 16)
		remote := &joiningNative{counts: []int{8, 16}}
		start := time.Now()
		var warmup time.Time
		e := experiment{
			request:   Request{Mode: "receiver"},
			cluster:   c,
			remote:    remote,
			topology:  layout,
			cell:      Cell{"livekit", "rtcbench"},
			dir:       t.TempDir(),
			remoteDir: "/dev/shm/rtcbench-bench/native-start",
			progress: func(phase string) error {
				if strings.HasPrefix(phase, "Warming") {
					warmup = time.Now()
				}
				return nil
			},
		}
		if err := e.prepare(t.Context()); err != nil {
			t.Fatal(err)
		}
		if remote.reads < 2 || warmup.Sub(start) < 5*time.Second || time.Since(warmup) != 3*time.Second {
			t.Fatalf("native measurement raced joins: reads=%d warmup=%v", remote.reads, warmup.Sub(start))
		}
	})
}
func TestNativeJoinTimeoutIsNotSteadyStateDegradation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _ := ParseCluster(testCluster)
		c.StartLeadSeconds, c.WarmupSeconds, c.StartupSeconds = 0, 0, 10
		layout, _ := Layout(c, "receiver", "livekit", 16)
		remote := &joiningNative{counts: []int{8}}
		e := experiment{
			request:   Request{Mode: "receiver"},
			cluster:   c,
			remote:    remote,
			topology:  layout,
			cell:      Cell{"livekit", "rtcbench"},
			dir:       t.TempDir(),
			remoteDir: "/dev/shm/rtcbench-bench/native-timeout",
			progress:  func(string) error { return nil },
		}
		trial, err := e.run(t.Context(), 1)
		if err != nil || trial.Passed || trial.Reason != "STARTUP_TIMEOUT" || len(trial.Samples) != 0 {
			t.Fatalf("%+v %v", trial, err)
		}
	})
}

func TestBrowserWarmupWaitsForStreamsAfterPagesLaunch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _ := ParseCluster(testCluster)
		c.StartLeadSeconds, c.WarmupSeconds = 0, 3
		layout, _ := Layout(c, "receiver", "livekit", 16)
		remote := &startingBrowsers{pages: []int{16}, streams: []int{8, 8, 16}}
		start := time.Now()
		var warmup time.Time
		e := experiment{
			request:   Request{Mode: "receiver"},
			cluster:   c,
			remote:    remote,
			topology:  layout,
			cell:      Cell{"livekit", "chromium"},
			dir:       t.TempDir(),
			remoteDir: "/dev/shm/rtcbench-bench/browser-stream-start",
			progress: func(phase string) error {
				if strings.HasPrefix(phase, "Warming") {
					warmup = time.Now()
				}
				return nil
			},
		}
		if err := e.prepare(t.Context()); err != nil {
			t.Fatal(err)
		}
		if warmup.Sub(start) < 5*time.Second || time.Since(warmup) != 3*time.Second {
			t.Fatalf("warmup raced stream readiness: %v", warmup.Sub(start))
		}
	})
}
