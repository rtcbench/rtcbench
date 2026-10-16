package api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type stoppingBenchmarks struct {
	stopping, done chan struct{}
}

func (b stoppingBenchmarks) Register(*http.ServeMux) {}
func (b stoppingBenchmarks) Cancel() <-chan struct{} { close(b.stopping); return b.done }

func TestShutdownCancelsRunsBeforeWaitingForBenchmarkCleanup(t *testing.T) {
	s, _ := newServer(t)
	stopping, release := make(chan struct{}), make(chan struct{})
	s.benchmarks = stoppingBenchmarks{stopping, release}
	normalCtx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	s.runs["normal"] = &run{Run: Run{Status: StatusRunning}, cancel: func() { cancel(); close(done) }, done: done}
	shutdown := make(chan struct{})
	go func() { s.Shutdown(context.Background()); close(shutdown) }()
	<-stopping
	select {
	case <-normalCtx.Done():
	case <-time.After(time.Second):
		t.Error("benchmark cleanup blocked cancellation of another run")
	}
	close(release)
	<-shutdown
}

func TestBenchmarkRoutesUseTheMainAPIServer(t *testing.T) {
	s, h := newServer(t)
	if response := do(t, h, "GET", "/api/v1/benchmarks", nil); response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("benchmark routes: %d %s", response.Code, response.Body)
	}
	s.Shutdown(t.Context())
	request := map[string]any{
		"name": "Capacity", "mode": "receiver", "dryRun": true, "repetitions": 1,
		"bounds":  map[string]int{"min": 1, "max": 2},
		"cells":   []map[string]string{{"sfu": "livekit", "client": "rtcbench"}},
		"cluster": "ssh_key: /keys/test\nssh_user: bench\nsfu: sfu\nsender: sender\nreceivers: [receiver]\n",
	}
	if response := do(t, h, "POST", "/api/v1/benchmarks", request); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("shutdown accepted benchmark: %d %s", response.Code, response.Body)
	}
}
