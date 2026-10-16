package bench

import (
	"context"
	"encoding/json"
	"net/url"
	"os/exec"
	"strings"
	"testing"
)

func TestBrowserLaunchPinsWholeContainerAndUsesConfiguredCamera(t *testing.T) {
	c, _ := ParseCluster(testCluster)
	c.Cores = "0-7"
	c.VideoPath = "/opt/ivf-videos/camera.mp4"
	for _, client := range Clients[1:] {
		layout, _ := Layout(c, "sender", "livekit", 66)
		remote := &recordingRemote{}
		e := experiment{
			request:   Request{Mode: "sender"},
			cluster:   c,
			remote:    remote,
			topology:  layout,
			cell:      Cell{SFU: "livekit", Client: client},
			dir:       t.TempDir(),
			remoteDir: "/dev/shm/rtcbench-bench/browser-001",
		}
		if err := e.browser(context.Background(), layout.Workers[0]); err != nil {
			t.Fatal(err)
		}
		commands := strings.Join(remote.commands, "\n")
		for _, want := range []string{"--cpuset-cpus '0-7'", "VIDEO_PATH=/opt/ivf-videos/camera.mp4", "--run-xvfb"} {
			if !strings.Contains(commands, want) {
				t.Fatalf("missing %s: %s", want, commands)
			}
		}
		var config struct {
			Sessions, MaxVideoDecoders             int
			VideoPath, URL, CustomURLHandler       string
			DetailedStatsPath, EnableDetailedStats string
		}
		for path, data := range remote.files {
			if strings.HasSuffix(path, "config.json") {
				if err := json.Unmarshal([]byte(data), &config); err != nil {
					t.Fatal(err)
				}
			}
		}
		if config.Sessions != 66 || config.VideoPath != c.VideoPath || config.CustomURLHandler == "" || config.DetailedStatsPath == "" || config.EnableDetailedStats != "0-65" {
			t.Fatalf("%+v", config)
		}
		if (client == "webrtcperf") != (config.MaxVideoDecoders == 0) {
			t.Fatalf("%s: %+v", client, config)
		}
		u, err := url.Parse(config.URL)
		if err != nil || u.Query().Get("secret") != "secret-secret-secret-secret-secret" || u.Query().Get("publish") != "true" || u.Query().Get("receive") != "false" {
			t.Fatal(config.URL, err)
		}
	}
}

func TestBrowserRoomHandlerMatchesWebRtcPerfContract(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	c, _ := ParseCluster(testCluster)
	for _, sfu := range SFUs {
		t.Run(sfu, func(t *testing.T) {
			layout := Topology{SFUIP: "192.0.2.10", Load: 21, PerRoom: 20}
			remote := &recordingRemote{}
			e := experiment{
				request:   Request{Mode: "receiver"},
				cluster:   c,
				remote:    remote,
				topology:  layout,
				cell:      Cell{sfu, "chromium"},
				dir:       t.TempDir(),
				remoteDir: "/dev/shm/rtcbench-bench/rooms",
			}
			if err := e.browser(t.Context(), "client"); err != nil {
				t.Fatal(err)
			}
			script := remote.files["client/dev/shm/rtcbench-bench/rooms/browser/rooms.js"]
			script += "\nconsole.log(module.exports({id:21,params:{}}));"
			output, err := exec.CommandContext(t.Context(), "node", "-e", script).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			if !strings.Contains(string(output), "1235") || !strings.Contains(string(output), "192.0.2.10") {
				t.Fatalf("wrong room URL: %s", output)
			}
		})
	}
}

func TestReceiverSkipsFakeCameraPreparation(t *testing.T) {
	c, _ := ParseCluster(testCluster)
	data, err := browserConfig(c, Topology{SFUIP: "192.0.2.10", Load: 1}, Cell{"livekit", "chromium"}, "receiver", "/tmp/bench")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if value, ok := config["videoPath"]; !ok || value != "" {
		t.Fatalf("receiver would prepare default fake media: %s", data)
	}
}
