package bench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHintsDoNotChangeBoundsOrMixModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hints.json")
	if err := os.WriteFile(path, []byte(`{"livekit/rtcbench":2,"sender/livekit/rtcbench":7,"_updated":1.5}`), 0600); err != nil {
		t.Fatal(err)
	}
	hints, err := LoadHints(path, "receiver")
	if err != nil || hints["livekit/rtcbench"] != 2 {
		t.Fatalf("%v %v", hints, err)
	}
	r := testRequest()
	r.Hints = hints
	report, err := Run(context.Background(), r, Options{Directory: filepath.Join(dir, "run"), Remote: forbiddenRemote{t}})
	if err != nil {
		t.Fatal(err)
	}
	result := report.Results["livekit/rtcbench"]
	if result.Best != 20 || result.Attempts[0].Load != 2 {
		t.Fatalf("%+v", result)
	}
	if err := SaveHints(path, "receiver", report); err != nil {
		t.Fatal(err)
	}
	unchanged, err := LoadHints(path, "receiver")
	if err != nil || unchanged["livekit/rtcbench"] != 2 {
		t.Fatal("simulation replaced measured hint", unchanged, err)
	}
	report.Simulated = false
	if err := SaveHints(path, "receiver", report); err != nil {
		t.Fatal(err)
	}
	updated, err := LoadHints(path, "receiver")
	if err != nil || updated["livekit/rtcbench"] != 20 {
		t.Fatal(updated, err)
	}
	loaded, err := LoadHints(path, "sender")
	if err != nil || loaded["livekit/rtcbench"] != 7 {
		t.Fatalf("%v %v", loaded, err)
	}
}

func TestRunnerSavesManifestAndRefusesToMixRuns(t *testing.T) {
	dir := t.TempDir()
	r := testRequest()
	_, err := Run(context.Background(), r, Options{Directory: dir, Remote: forbiddenRemote{t}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"request.json", "cluster.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Run(context.Background(), r, Options{Directory: dir, Remote: forbiddenRemote{t}}); err == nil {
		t.Fatal("mixed two runs in one directory")
	}
}
