package bench

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type provisioningRemote struct {
	recordingRemote
	failHealth bool
	uploads    []string
}

func (r *provisioningRemote) Run(ctx context.Context, host, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	output, err := r.recordingRemote.Run(ctx, host, command)
	if r.failHealth && strings.Contains(command, "seq 60") {
		return "", errors.New("unhealthy SFU")
	}
	return output, err
}

func (r *provisioningRemote) Upload(ctx context.Context, host, path string, content io.Reader, executable bool) error {
	if _, err := io.ReadAll(content); err != nil {
		return err
	}
	r.uploads = append(r.uploads, host+":"+path)
	return ctx.Err()
}

func provisionCluster() Cluster {
	c, _ := ParseCluster(testRequest().ClusterYAML)
	c.Jitsi.Prosody, c.Jitsi.Jicofo, c.Jitsi.JVB = "prosody", "jicofo", Hosts{"bridge1", "bridge2"}
	return c
}

func TestProvisioningServicesAndCleanup(t *testing.T) {
	for _, sfu := range SFUs {
		t.Run(sfu, func(t *testing.T) {
			r := &provisioningRemote{}
			p := provisioner{cluster: provisionCluster(), remote: r, id: "trial-001"}
			if err := p.start(t.Context(), sfu); err != nil {
				t.Fatal(err)
			}
			started := strings.Join(r.commands, "\n")
			if !strings.Contains(started, "seq 60") {
				t.Fatal("SFU readiness was not checked")
			}
			if sfu == "jitsi" {
				for _, host := range []string{"prosody", "jicofo", "bridge1", "bridge2"} {
					if !strings.Contains(started, host+": docker run") {
						t.Errorf("missing service on %s", host)
					}
				}
			}
			r.commands = nil
			if err := p.stop(t.Context(), sfu); err != nil {
				t.Fatal(err)
			}
			for _, command := range r.commands {
				if !strings.Contains(command, "trial-001") || strings.Contains(command, "docker ps") || strings.Contains(command, "pkill") {
					t.Errorf("cleanup is not limited to this trial: %s", command)
				}
			}
		})
	}
}

func TestProvisioningFailureStillCleansUp(t *testing.T) {
	r := &provisioningRemote{failHealth: true}
	c := provisionCluster()
	req := testRequest()
	req.Provision, req.DryRun = true, false
	topology, err := Layout(c, req.Mode, "livekit", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := experiment{
		request:   req,
		cluster:   c,
		remote:    r,
		topology:  topology,
		cell:      Cell{"livekit", "rtcbench"},
		dir:       t.TempDir(),
		remoteDir: "/dev/shm/rtcbench-bench/failed-001",
		log:       io.Discard,
		progress:  func(string) error { return nil },
	}
	trial, err := e.run(t.Context(), 1)
	if err == nil || trial.Passed || !strings.Contains(err.Error(), "unhealthy SFU") {
		t.Fatalf("trial: %+v, error: %v", trial, err)
	}
	commands := strings.Join(r.commands, "\n")
	if !strings.Contains(commands, "readlink /proc/") || !strings.Contains(commands, "docker rm -f 'rtcbench-failed-001-web'") {
		t.Fatal("failed start leaked provisioning resources", commands)
	}
}

func TestAssetUploadValidatesBeforeTouchingHosts(t *testing.T) {
	c := provisionCluster()
	c.BinariesDir, c.IVFDir = t.TempDir(), t.TempDir()
	r := &provisioningRemote{}
	p := provisioner{cluster: c, remote: r, id: "assets"}
	if err := p.upload(t.Context(), []Cell{{SFU: "livekit", Client: "rtcbench"}}); err == nil {
		t.Fatal("missing binaries accepted")
	}
	if len(r.commands) != 0 || len(r.uploads) != 0 {
		t.Fatal("touched hosts before validation")
	}
	for _, name := range []string{"rtcbench", "livekit-server"} {
		if err := os.WriteFile(filepath.Join(c.BinariesDir, name), []byte("binary"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(c.IVFDir, "video.ivf"), []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.upload(t.Context(), []Cell{{SFU: "livekit", Client: "rtcbench"}}); err != nil {
		t.Fatal(err)
	}
	if len(r.uploads) == 0 {
		t.Fatal("no assets uploaded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.start(ctx, "livekit"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestProvisioningShellCommandsParse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote commands use the Linux shell")
	}
	for _, sfu := range SFUs {
		r := &provisioningRemote{}
		p := provisioner{cluster: provisionCluster(), remote: r, id: "syntax-001"}
		p.cluster.DataIPs = map[string]string{"192.0.2.1": "10.2.96.1", "192.0.2.2": "10.2.96.2"}
		if err := p.start(t.Context(), sfu); err != nil {
			t.Fatal(err)
		}
		if err := p.stop(t.Context(), sfu); err != nil {
			t.Fatal(err)
		}
		for _, recorded := range r.commands {
			_, command, _ := strings.Cut(recorded, ": ")
			cmd := exec.CommandContext(t.Context(), "sh", "-n")
			cmd.Stdin = strings.NewReader(command)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s\n%s", sfu, err, output, command)
			}
		}
	}
}

func TestProvisioningRejectsUnsupportedRoutesAndPorts(t *testing.T) {
	for _, settings := range []string{
		"data_ips:\n  192.0.2.1: '2001:db8::1'\n",
		"data_ips:\n  sfu: 10.2.96.1\n",
		"mediasoup_ws_port: 8443\n",
	} {
		r := testRequest()
		r.Cells = []Cell{{"mediasoup", "rtcbench"}}
		r.ClusterYAML += settings
		r.Provision = true
		if _, err := r.Validate(); err == nil {
			t.Fatalf("accepted provisioning with %s", settings)
		}
		r.Provision = false
		if _, err := r.Validate(); err != nil {
			t.Fatalf("external deployment rejected: %v", err)
		}
	}
}

func TestCollectsSFULogsBeforeRemovingServices(t *testing.T) {
	r := &recordingRemote{}
	e := experiment{
		cluster:     provisionCluster(),
		remote:      r,
		cell:        Cell{"janus", "rtcbench"},
		provisioned: true,
		dir:         t.TempDir(),
		remoteDir:   "/dev/shm/rtcbench-bench/logs-001",
	}
	if err := e.finish(); err != nil {
		t.Fatal(err)
	}
	collected := false
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "docker logs 'rtcbench-logs-001-sfu'") {
			collected = true
		}
		if strings.Contains(cmd, "docker rm -f 'rtcbench-logs-001-sfu'") && !collected {
			t.Fatal("SFU removed before its failure evidence was collected")
		}
	}
	if !collected {
		t.Fatal("missing SFU logs")
	}
}

func TestAssetUploadNeedsOnlySelectedPluginBinaries(t *testing.T) {
	c := provisionCluster()
	c.BinariesDir, c.IVFDir = t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(c.BinariesDir, "rtcbench"), []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	remote := &provisioningRemote{}
	p := provisioner{cluster: c, remote: remote, id: "selected-assets"}
	cells := []Cell{{SFU: "janus", Client: "rtcbench"}}
	if err := p.upload(t.Context(), cells); err != nil {
		t.Fatal(err)
	}
	for _, uploaded := range remote.uploads {
		if strings.Contains(uploaded, "livekit-server") {
			t.Fatal("uploaded an unselected plugin", uploaded)
		}
	}
	cells = append(cells, Cell{SFU: "livekit", Client: "chromium"})
	remote.uploads = nil
	if err := p.upload(t.Context(), cells); err == nil {
		t.Fatal("missing selected plugin binary accepted")
	}
	if len(remote.uploads) != 0 {
		t.Fatal("uploaded before validating selected assets")
	}
}
