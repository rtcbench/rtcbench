package pionutil

import (
	"testing"
	"time"
)

func TestPacketSchedulerVariesJitter(t *testing.T) {
	profile := &ImpairmentProfile{
		Name:         "jittery",
		BaseLatency:  20 * time.Millisecond,
		JitterStddev: 8 * time.Millisecond,
		Seed:         7,
	}

	delivered := make(chan time.Time, 8)
	scheduler := newPacketScheduler(profile, "test/downlink", func([]byte) {
		delivered <- time.Now()
	})
	defer scheduler.Close()

	delays := make([]time.Duration, 8)
	for i := range delays {
		started := time.Now()
		scheduler.Enqueue([]byte("ping"))
		select {
		case at := <-delivered:
			delays[i] = at.Sub(started)
		case <-time.After(250 * time.Millisecond):
			t.Fatal("timed out waiting for scheduled packet")
		}
	}

	minDelay := delays[0]
	maxDelay := delays[0]
	for _, delay := range delays[1:] {
		if delay < minDelay {
			minDelay = delay
		}
		if delay > maxDelay {
			maxDelay = delay
		}
	}
	if maxDelay-minDelay < 10*time.Millisecond {
		t.Fatalf("delay spread = %s, want at least 10ms", maxDelay-minDelay)
	}
}

func TestTransmitDuration(t *testing.T) {
	got := transmitDuration(1500, 1_000_000)
	want := 12 * time.Millisecond
	if got != want {
		t.Fatalf("transmitDuration() = %s, want %s", got, want)
	}
}
