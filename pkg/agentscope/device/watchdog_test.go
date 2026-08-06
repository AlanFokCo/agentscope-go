package device

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdog_KickPreventsTriggering(t *testing.T) {
	var triggered atomic.Int32
	wd := NewWatchdog(50*time.Millisecond, func() {
		triggered.Add(1)
	})

	wd.Start()
	defer wd.Stop()

	// Kick several times within the timeout
	for i := 0; i < 5; i++ {
		time.Sleep(20 * time.Millisecond)
		wd.Kick()
	}

	// Wait a bit more (but not past timeout since last kick)
	time.Sleep(30 * time.Millisecond)

	if triggered.Load() != 0 {
		t.Error("watchdog should not have triggered while being kicked")
	}
}

func TestWatchdog_TriggersOnTimeout(t *testing.T) {
	var triggered atomic.Int32
	wd := NewWatchdog(30*time.Millisecond, func() {
		triggered.Add(1)
	})

	wd.Start()

	// Don't kick — wait for timeout
	time.Sleep(60 * time.Millisecond)

	if triggered.Load() != 1 {
		t.Errorf("expected watchdog to trigger once, got %d", triggered.Load())
	}

	if wd.Running() {
		t.Error("watchdog should not be running after trigger")
	}
}

func TestWatchdog_StopPreventsTriggering(t *testing.T) {
	var triggered atomic.Int32
	wd := NewWatchdog(30*time.Millisecond, func() {
		triggered.Add(1)
	})

	wd.Start()
	time.Sleep(10 * time.Millisecond)
	wd.Stop()

	// Wait past the original timeout
	time.Sleep(40 * time.Millisecond)

	if triggered.Load() != 0 {
		t.Error("watchdog should not trigger after Stop()")
	}
}

func TestWatchdog_DoubleStart(t *testing.T) {
	var triggered atomic.Int32
	wd := NewWatchdog(50*time.Millisecond, func() {
		triggered.Add(1)
	})

	wd.Start()
	wd.Start() // should be no-op
	wd.Stop()

	time.Sleep(60 * time.Millisecond)
	if triggered.Load() != 0 {
		t.Error("double start should not create extra timers")
	}
}
