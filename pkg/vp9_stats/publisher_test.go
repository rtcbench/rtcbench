package vp9_stats

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPeriod_String(t *testing.T) {
	p := Period{StartTimeUS: 1000000, TotalBytes: 5000}
	got := p.String()
	want := "vp9_stats.Period[startTimeUS=1000000;totalBytes=5000]"
	if got != want {
		t.Fatalf("Period.String() = %q, want %q", got, want)
	}
}

func TestNewPublisher(t *testing.T) {
	ch := make(chan VideoQualitySample, 1)
	pub := NewPublisher(ch)
	if pub == nil {
		t.Fatalf("NewPublisher returned nil")
	}
}

func TestPublisher_FanOut(t *testing.T) {
	ch := make(chan VideoQualitySample, 10)
	pub := NewPublisher(ch)

	const wantCount = 3

	var mu1 sync.Mutex
	var samples1 []VideoQualitySample
	var periods1 []Period

	var mu2 sync.Mutex
	var samples2 []VideoQualitySample
	var periods2 []Period

	allReceived := make(chan struct{})

	pub.AddSubscriber(func(p Period, s VideoQualitySample) {
		mu1.Lock()
		defer mu1.Unlock()
		samples1 = append(samples1, s)
		periods1 = append(periods1, p)
	})

	pub.AddSubscriber(func(p Period, s VideoQualitySample) {
		mu2.Lock()
		defer mu2.Unlock()
		samples2 = append(samples2, s)
		periods2 = append(periods2, p)
		if len(samples2) == wantCount {
			close(allReceived)
		}
	})

	go pub.Run()

	ch <- VideoQualitySample{Sample: SampleData{TotalBytes: 100}}
	ch <- VideoQualitySample{Sample: SampleData{TotalBytes: 200}}
	ch <- VideoQualitySample{Sample: SampleData{TotalBytes: 300}}

	// Wait until subscribers have received all samples before stopping
	select {
	case <-allReceived:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for subscribers to receive all samples")
	}

	done := make(chan struct{})
	go func() {
		pub.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for publisher to stop")
	}

	mu1.Lock()
	defer mu1.Unlock()
	mu2.Lock()
	defer mu2.Unlock()

	if len(samples1) != wantCount {
		t.Fatalf("subscriber 1 received %d samples, want %d", len(samples1), wantCount)
	}
	if len(samples2) != wantCount {
		t.Fatalf("subscriber 2 received %d samples, want %d", len(samples2), wantCount)
	}

	// Verify TotalBytes values received by subscriber 1
	wantBytes := []int64{100, 200, 300}
	for i, want := range wantBytes {
		if samples1[i].Sample.TotalBytes != want {
			t.Fatalf("subscriber 1 sample[%d].Sample.TotalBytes = %d, want %d", i, samples1[i].Sample.TotalBytes, want)
		}
		if samples2[i].Sample.TotalBytes != want {
			t.Fatalf("subscriber 2 sample[%d].Sample.TotalBytes = %d, want %d", i, samples2[i].Sample.TotalBytes, want)
		}
	}

	// Last period should have accumulated TotalBytes = 100+200+300 = 600
	lastPeriod := periods1[len(periods1)-1]
	if lastPeriod.TotalBytes != 600 {
		t.Fatalf("last Period.TotalBytes = %d, want 600", lastPeriod.TotalBytes)
	}
}

func TestErrIDToString_AllIDs(t *testing.T) {
	for id := 0; id < NumErrIDs; id++ {
		s := ErrIDToString(id)
		if s == "" {
			t.Fatalf("ErrIDToString(%d) returned empty string", id)
		}
		if !strings.HasPrefix(s, "vp9_stats.Publisher.") {
			t.Fatalf("ErrIDToString(%d) = %q, want prefix 'vp9_stats.Publisher.'", id, s)
		}
	}
	// smoke test a specific one
	if got := ErrIDToString(ErrViewerSeqNoJumpID); got != "vp9_stats.Publisher.ErrViewerSeqNoJumpID" {
		t.Fatalf("ErrIDToString(ErrViewerSeqNoJumpID) = %q, want %q", got, "vp9_stats.Publisher.ErrViewerSeqNoJumpID")
	}
}

func TestErrIDToString_InvalidPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for invalid errID")
		}
	}()
	ErrIDToString(NumErrIDs)
}

func TestPublisher_Stop_Idempotent(t *testing.T) {
	ch := make(chan VideoQualitySample, 1)
	pub := NewPublisher(ch)

	go pub.Run()

	done := make(chan struct{})
	go func() {
		pub.Stop()
		pub.Stop()
		close(done)
	}()

	select {
	case <-done:
		// no panic, success
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out: Stop() likely deadlocked or panicked")
	}
}

func TestPublisher_NoSubscribers(t *testing.T) {
	ch := make(chan VideoQualitySample, 10)
	pub := NewPublisher(ch)

	go pub.Run()

	ch <- VideoQualitySample{Sample: SampleData{TotalBytes: 100}}
	ch <- VideoQualitySample{Sample: SampleData{TotalBytes: 200}}

	done := make(chan struct{})
	go func() {
		pub.Stop()
		close(done)
	}()

	select {
	case <-done:
		// no panic, success
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out: publisher with no subscribers likely deadlocked")
	}
}
