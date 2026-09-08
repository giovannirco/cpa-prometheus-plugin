package quota

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestNewPollerDefaultIntervalIsFiveMinutes(t *testing.T) {
	p := NewPoller(0)
	if p.Interval != 5*time.Minute {
		t.Fatalf("Interval = %s, want 5m", p.Interval)
	}
	if DefaultRefreshInterval != 5*time.Minute {
		t.Fatalf("DefaultRefreshInterval = %s, want 5m", DefaultRefreshInterval)
	}
}

func TestPollerStartRunsImmediatelyThenStops(t *testing.T) {
	p := NewPoller(time.Hour)
	var n atomic.Int32
	p.Start(func() { n.Add(1) })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && n.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if n.Load() < 1 {
		t.Fatal("poller did not run immediately")
	}
	p.Stop()
}

func TestStopWithoutStartDoesNotBlock(t *testing.T) {
	p := NewPoller(time.Minute)
	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked on a poller that was never started")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	p := NewPoller(time.Minute)
	p.Start(func() {})
	p.Stop()
	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second Stop blocked")
	}
}

func TestStartAfterStopDoesNotRun(t *testing.T) {
	p := NewPoller(time.Minute)
	p.Stop()
	var ran int32
	p.Start(func() { atomic.AddInt32(&ran, 1) })
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&ran) != 0 {
		t.Fatal("Start ran the callback after Stop")
	}
}
