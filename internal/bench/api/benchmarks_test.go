package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rtcbench/rtcbench/internal/bench"
)

func benchmarkRequest() bench.Request {
	return bench.Request{
		Name:        "Capacity",
		Mode:        "receiver",
		DryRun:      true,
		Repetitions: 1,
		Bounds:      bench.Bounds{Min: 1, Max: 30},
		Cells:       []bench.Cell{{SFU: "livekit", Client: "rtcbench"}},
		ClusterYAML: "ssh_key: /keys/test\nssh_user: bench\nsfu: sfu\nsender: sender\nreceivers: [receiver]\n",
	}
}

func waitBenchmark(t *testing.T, s *Service, id string) {
	t.Helper()
	s.mu.Lock()
	done := s.benchmarks[id].done
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("benchmark did not stop")
	}
}

func startBenchmark(t *testing.T, s *Service, h http.Handler) Benchmark {
	t.Helper()
	response := do(t, h, http.MethodPost, "/api/v1/benchmarks", benchmarkRequest())
	if response.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", response.Code, response.Body)
	}
	var b Benchmark
	if err := json.Unmarshal(response.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBenchmarkAPIStoresAndReloadsResults(t *testing.T) {
	s, h := newServer(t)
	b := startBenchmark(t, s, h)
	waitBenchmark(t, s, b.ID)
	path := "/api/v1/benchmarks/" + b.ID
	if err := json.Unmarshal(do(t, h, "GET", path, nil).Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusCompleted || b.Report.Results["livekit/rtcbench"].Best != 20 || b.EndedAt == nil {
		t.Fatalf("%+v", b)
	}
	for _, file := range []string{"experiments.jsonl", "summary.json", "benchmark.json", "benchmark.log"} {
		if response := do(t, h, "GET", path+"/files/"+file, nil); response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", file, response.Code, response.Body)
		}
	}
	var files []File
	response := do(t, h, "GET", path+"/files", nil)
	if err := json.Unmarshal(response.Body.Bytes(), &files); err != nil || len(files) < 4 {
		t.Fatalf("files: %v %s", err, response.Body)
	}
	if response := do(t, h, "GET", path+"/files/%2e%2e/secret", nil); response.Code == http.StatusOK {
		t.Fatal("file traversal accepted")
	}
	reopened, err := New(s.dir, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.benchmarks[b.ID].Status != StatusCompleted {
		t.Fatal("lost completed benchmark")
	}
	if code := do(t, h, "DELETE", path, nil).Code; code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "benchmarks", b.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("result remains: %v", err)
	}
}

func TestBenchmarkAPICancellationAndExclusion(t *testing.T) {
	s, h := newServer(t)
	started := make(chan struct{})
	s.benchmarkRun = func(ctx context.Context, _ bench.Request, options bench.Options) (bench.Report, error) {
		report := bench.Report{Phase: "Measuring", Results: map[string]bench.SearchResult{}, Trials: []bench.Trial{}}
		if err := options.Update(report); err != nil {
			return report, err
		}
		close(started)
		<-ctx.Done()
		return report, ctx.Err()
	}
	b := startBenchmark(t, s, h)
	<-started
	if code := do(t, h, "POST", "/api/v1/benchmarks", benchmarkRequest()).Code; code != http.StatusConflict {
		t.Fatalf("concurrent benchmark: %d", code)
	}
	path := "/api/v1/benchmarks/" + b.ID
	if code := do(t, h, "DELETE", path, nil).Code; code != http.StatusConflict {
		t.Fatalf("deleted active benchmark: %d", code)
	}
	if code := do(t, h, "POST", path+"/stop", nil).Code; code != http.StatusAccepted {
		t.Fatalf("stop: %d", code)
	}
	waitBenchmark(t, s, b.ID)
	_ = json.Unmarshal(do(t, h, "GET", path, nil).Body.Bytes(), &b)
	if b.Status != StatusInterrupted || b.Report.Phase != "Stopped" {
		t.Fatalf("%+v", b)
	}
}

func TestBenchmarkRestartMarksActiveJobInterrupted(t *testing.T) {
	s, h := newServer(t)
	b := startBenchmark(t, s, h)
	waitBenchmark(t, s, b.ID)
	b.Status = StatusRunning
	if err := s.saveBenchmark(b); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(s.dir, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.benchmarks[b.ID].Status != StatusInterrupted {
		t.Fatal("restart retained a running job")
	}
}

func TestBenchmarkRejectsInvalidRequestsBeforeStarting(t *testing.T) {
	s, h := newServer(t)
	for _, change := range []func(*bench.Request){
		func(r *bench.Request) { r.Name = "../outside" },
		func(r *bench.Request) { r.Repetitions = 0 },
		func(r *bench.Request) { r.Cells = nil },
		func(r *bench.Request) { r.ClusterYAML = "not: cluster" },
	} {
		req := benchmarkRequest()
		change(&req)
		if code := do(t, h, "POST", "/api/v1/benchmarks", req).Code; code != http.StatusBadRequest {
			t.Fatalf("accepted invalid request: %d", code)
		}
	}
	if len(s.benchmarks) != 0 {
		t.Fatal("created invalid jobs")
	}
}

func newServer(t *testing.T) (*Service, http.Handler) {
	t.Helper()
	s, err := New(t.TempDir(), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { <-s.Cancel() })
	mux := http.NewServeMux()
	s.Register(mux)
	return s, mux
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(data)))
	return w
}

func TestCancelRejectsNewJobsWhileWaitingForCleanup(t *testing.T) {
	s, h := newServer(t)
	started, release := make(chan struct{}), make(chan struct{})
	s.benchmarkRun = func(ctx context.Context, _ bench.Request, _ bench.Options) (bench.Report, error) {
		close(started)
		<-ctx.Done()
		<-release
		return bench.Report{}, ctx.Err()
	}
	b := startBenchmark(t, s, h)
	<-started
	done := s.Cancel()
	select {
	case <-done:
		t.Error("returned before cleanup")
	default:
	}
	if code := do(t, h, "POST", "/api/v1/benchmarks", benchmarkRequest()).Code; code != http.StatusServiceUnavailable {
		t.Errorf("start during shutdown: %d", code)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	waitBenchmark(t, s, b.ID)
}
