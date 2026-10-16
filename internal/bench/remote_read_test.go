package bench

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

type connectionFailure struct{}

func (*connectionFailure) Error() string { return "SSH connection timed out during banner exchange" }
func (*connectionFailure) ExitCode() int { return 255 }

type recoveringOOM struct {
	recordingRemote
	attempts int
}

func (r *recoveringOOM) Run(context.Context, string, string) (string, error) {
	r.attempts++
	if r.attempts <= 2 {
		return "", &connectionFailure{}
	}
	return "{\"Running\":false,\"OOMKilled\":true}", nil
}

func TestOOMDetectionSurvivesTemporarySSHFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		remote := &recoveringOOM{}
		e := experiment{remote: remote}
		start := time.Now()
		_, err := e.browserStarted(t.Context(), container{host: "client", name: "load"})
		failure, ok := err.(*startupFailure)
		if !ok || failure.reason != "OOM" || remote.attempts != 3 || time.Since(start) != 4*time.Second {
			t.Fatalf("attempts=%d elapsed=%v err=%v", remote.attempts, time.Since(start), err)
		}
	})
}

type failingRead struct {
	recordingRemote
	failure  error
	attempts int
}

func (r *failingRead) Run(context.Context, string, string) (string, error) {
	r.attempts++
	return "", r.failure
}

func TestReadRetryStopsAfterFiveConnectionFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := &connectionFailure{}
		remote := &failingRead{failure: failure}
		e := experiment{remote: remote}
		start := time.Now()
		_, err := e.readRemote(t.Context(), "client", "read")
		if err != failure || remote.attempts != 5 || time.Since(start) != 8*time.Second {
			t.Fatalf("attempts=%d elapsed=%v err=%v", remote.attempts, time.Since(start), err)
		}
	})
}

func TestReadDoesNotRetryCommandFailure(t *testing.T) {
	failure := context.DeadlineExceeded
	remote := &failingRead{failure: failure}
	e := experiment{remote: remote}
	_, err := e.readRemote(t.Context(), "client", "read")
	if err != failure || remote.attempts != 1 {
		t.Fatal(remote.attempts, err)
	}
}

func TestReadRetryHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		remote := &failingRead{failure: &connectionFailure{}}
		e := experiment{remote: remote}
		ctx, cancel := context.WithCancel(t.Context())
		go func() { time.Sleep(time.Second); cancel() }()
		start := time.Now()
		_, err := e.readRemote(ctx, "client", "read")
		if err != context.Canceled || remote.attempts != 1 || time.Since(start) != time.Second {
			t.Fatalf("attempts=%d elapsed=%v err=%v", remote.attempts, time.Since(start), err)
		}
	})
}
