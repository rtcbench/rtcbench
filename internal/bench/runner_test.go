package bench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCluster = `ssh_key: /keys/bench
ssh_user: bench
sfu: 192.0.2.1
sender: 192.0.2.2
receivers: [192.0.2.3]
senders: [192.0.2.3]
viewer: 192.0.2.2
`

func testRequest() Request {
	return Request{
		ClusterYAML: testCluster,
		Mode:        "receiver",
		Cells:       []Cell{{SFU: "livekit", Client: "rtcbench"}},
		Bounds:      Bounds{Min: 1, Max: 40},
		Repetitions: 2,
		DryRun:      true,
	}
}

func TestRunRequiresEveryRepetitionToPass(t *testing.T) {
	req := testRequest()
	req.DryRun = false
	req.ClusterYAML += "teardown_seconds: 0\n"
	req.Bounds = Bounds{Min: 1, Max: 8}
	var calls []TrialSpec
	report, err := Run(context.Background(), req, Options{
		Directory: t.TempDir(),
		Remote:    forbiddenRemote{t},
		Execute: func(_ context.Context, spec TrialSpec) (Trial, error) {
			calls = append(calls, spec)
			return Trial{Passed: spec.Topology.Load <= 5 || spec.Repeat == 1, Reason: "OK"}, nil
		},
	})
	if err != nil || report.Results["livekit/rtcbench"].Best != 5 {
		t.Fatalf("%+v %v", report, err)
	}
	for i := 0; i < len(calls); i += 2 {
		if calls[i].Topology.Load != calls[i+1].Topology.Load || calls[i].Repeat != 1 || calls[i+1].Repeat != 2 {
			t.Fatalf("repetitions: %+v", calls)
		}
	}
}

func TestRunAbortsInfrastructureFailureAndRecordsIt(t *testing.T) {
	req := testRequest()
	req.DryRun = false
	want := errors.New("cannot inspect container")
	report, err := Run(context.Background(), req, Options{
		Directory: t.TempDir(),
		Execute:   func(context.Context, TrialSpec) (Trial, error) { return Trial{}, want },
	})
	if !errors.Is(err, want) || len(report.Trials) != 1 || report.Trials[0].Passed || !strings.Contains(report.Trials[0].Error, want.Error()) {
		t.Fatalf("%+v %v", report, err)
	}
	if len(report.Results["livekit/rtcbench"].Attempts) != 0 {
		t.Fatal("infrastructure failure moved the search boundary")
	}
}

func TestValidationRejectsOverlappingMachinesAndNonFiniteThresholds(t *testing.T) {
	for _, edit := range []func(*Request){
		func(r *Request) {
			r.ClusterYAML = strings.ReplaceAll(testCluster, "receivers: [192.0.2.3]", "receivers: [192.0.2.2]")
		},
		func(r *Request) {
			r.ClusterYAML = strings.ReplaceAll(testCluster, "receivers: [192.0.2.3]", "receivers: [192.0.2.1]")
		},
		func(r *Request) {
			r.ClusterYAML = strings.ReplaceAll(testCluster, "receivers: [192.0.2.3]", "receivers: [192.0.2.3, 192.0.2.3]")
		},
		func(r *Request) { r.ClusterYAML += "min_fps: .nan\n" },
		func(r *Request) { r.ClusterYAML += "min_bitrate_bps: .inf\n" },
		func(r *Request) { r.ClusterYAML += "cores: 7-0\n" },
		func(r *Request) { r.ClusterYAML += "cores: 0-7,2\n" },
		func(r *Request) { r.ClusterYAML += "initial_hi:\n  rtcbench: 0\n" },
	} {
		r := testRequest()
		edit(&r)
		if _, err := r.Validate(); err == nil {
			t.Fatalf("accepted invalid config: %s", r.ClusterYAML)
		}
	}
}

func TestNetworkCeilingUsesAllLoadMachines(t *testing.T) {
	r := testRequest()
	r.ClusterYAML = strings.ReplaceAll(testCluster, "receivers: [192.0.2.3]", "receivers: [192.0.2.3, 192.0.2.4]") + "nic_mbps: 100\n"
	report, err := Run(context.Background(), r, Options{Directory: t.TempDir(), Remote: forbiddenRemote{t}})
	if err != nil {
		t.Fatal(err)
	}
	result := report.Results["livekit/rtcbench"]
	if result.Best != 14 || !result.CeilingReached {
		t.Fatalf("network cap should be 100/(2*3.5): %+v", result)
	}
}

type forbiddenRemote struct{ t *testing.T }

func (r forbiddenRemote) Run(context.Context, string, string) (string, error) {
	r.t.Fatal("dry-run contacted a host")
	return "", nil
}
func (r forbiddenRemote) Write(context.Context, string, string, []byte) error {
	r.t.Fatal("dry-run wrote a remote file")
	return nil
}

func TestDryRunNeverContactsHostsAndPersistsTrials(t *testing.T) {
	dir := t.TempDir()
	req := testRequest()
	req.Provision = true
	req.UploadAssets = true
	report, err := Run(context.Background(), req, Options{Directory: dir, Remote: forbiddenRemote{t}})
	if err != nil || report.Results["livekit/rtcbench"].Best != 20 {
		t.Fatalf("%+v %v", report, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Report
	if err := json.Unmarshal(data, &saved); err != nil || len(saved.Trials) != len(report.Trials) {
		t.Fatalf("%+v %v", saved, err)
	}
	for _, trial := range saved.Trials {
		if trial.Reason != "SIMULATED" {
			t.Fatalf("unmarked simulation: %+v", trial)
		}
	}
}

func TestRunnerPropagatesPersistenceFailure(t *testing.T) {
	want := errors.New("disk full")
	_, err := Run(context.Background(), testRequest(), Options{
		Directory: t.TempDir(),
		Update:    func(Report) error { return want },
		Remote:    forbiddenRemote{t},
	})
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
}

func TestTopologyUsesExactParticipantCounts(t *testing.T) {
	c, _ := ParseCluster(testCluster)
	for _, mode := range []string{"receiver", "sender"} {
		for _, sfu := range []string{"livekit", "janus", "mediasoup"} {
			for n := 1; n < 200; n++ {
				layout, err := Layout(c, mode, sfu, n)
				if err != nil {
					t.Fatal(err)
				}
				actual := 0
				for _, g := range layout.Groups {
					actual += g.Rooms * g.Users
				}
				if actual != n || layout.ExpectedStreams != n {
					t.Fatalf("%s %s %d: %+v", mode, sfu, n, layout)
				}
			}
		}
	}
}

func TestConfigValidationRejectsUnsafeOrMissingSettings(t *testing.T) {
	for _, extra := range []string{
		"\nssh_user: 'root; touch /tmp/oops'",
		"\nsample_seconds: 0",
		"\nmin_fps: -1",
		"\ncores: '0-7; id'",
		"\nunknown: true",
	} {
		r := testRequest()
		r.ClusterYAML += extra
		if _, err := r.Validate(); err == nil {
			t.Fatalf("accepted %s", extra)
		}
	}
}
