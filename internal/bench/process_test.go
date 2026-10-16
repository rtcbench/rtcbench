package bench

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	mode := os.Getenv("RTCBENCH_PROCESS_TEST")
	if mode == "" {
		return
	}
	if mode == "child" {
		fmt.Println("child ready")
		time.Sleep(1500 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("RTCBENCH_PROCESS_MARKER"), []byte("still running"), 0600)
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$")
	child.Env = append(os.Environ(), "RTCBENCH_PROCESS_TEST=child")
	child.Stdout = os.Stdout
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
}

func TestCancellationStopsChildProcesses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := filepath.Join(t.TempDir(), "survived")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessHelper$")
	cmd.Env = append(os.Environ(), "RTCBENCH_PROCESS_TEST=parent", "RTCBENCH_PROCESS_MARKER="+marker)
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := bufio.NewScanner(stdout)
	if !ready.Scan() || ready.Text() != "child ready" {
		t.Fatal("child did not start")
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("command ignored cancellation")
	}
	time.Sleep(1600 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child survived cancellation: %v", err)
	}
}
