package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIExercisesGoSearchAndAnalysis(t *testing.T) {
	dir := t.TempDir()
	cluster := filepath.Join(dir, "cluster.yml")
	if err := os.WriteFile(cluster, []byte("ssh_key: /keys/test\nssh_user: bench\nsfu: 192.0.2.1\nsender: 192.0.2.2\nreceivers: [192.0.2.3]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	results := filepath.Join(dir, "results")
	err := run(context.Background(), []string{
		cluster,
		"--cells",
		"livekit:rtcbench,chromium",
		"--min-r",
		"1",
		"--max-r",
		"25",
		"--dry-run",
		"--out",
		results,
	}, &output, io.Discard)
	if err != nil || !strings.Contains(output.String(), "SIMULATION") || !strings.Contains(output.String(), "livekit/rtcbench: 20") {
		t.Fatalf("%v %s", err, &output)
	}
	output.Reset()
	if err := run(context.Background(), []string{"analyze", results}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "livekit/rtcbench") {
		t.Fatal("analysis reported simulations as measurements")
	}
}
