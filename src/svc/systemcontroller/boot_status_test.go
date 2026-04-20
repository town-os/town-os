package systemcontroller

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// drain collects all events from ch until it closes or the deadline
// fires. Used to assert final subscriber state after Done() / cancel().
func drain(ch <-chan progressEvent, deadline time.Duration) []progressEvent {
	out := []progressEvent{}
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, evt)
		case <-timer.C:
			return out
		}
	}
}

func TestBootStatus_BroadcastsToAllSubscribers(t *testing.T) {
	bs := NewBootStatus()
	a, cancelA := bs.Subscribe()
	defer cancelA()
	b, cancelB := bs.Subscribe()
	defer cancelB()

	bs.Step("one")
	bs.Step("two")
	bs.Done()

	gotA := drain(a, 100*time.Millisecond)
	gotB := drain(b, 100*time.Millisecond)

	want := []progressEvent{{Step: "one"}, {Step: "two"}, {Done: true}}
	for i, evt := range want {
		if i >= len(gotA) || gotA[i] != evt {
			t.Errorf("subscriber A event %d = %+v, want %+v; full=%+v", i, gotA[i], evt, gotA)
		}
		if i >= len(gotB) || gotB[i] != evt {
			t.Errorf("subscriber B event %d = %+v, want %+v; full=%+v", i, gotB[i], evt, gotB)
		}
	}
}

func TestBootStatus_LateSubscriberSeesHistory(t *testing.T) {
	bs := NewBootStatus()
	bs.Step("one")
	bs.Step("two")

	ch, cancel := bs.Subscribe()
	defer cancel()
	bs.Step("three")

	got := []progressEvent{}
	deadline := time.After(100 * time.Millisecond)
	for len(got) < 3 {
		select {
		case evt := <-ch:
			got = append(got, evt)
		case <-deadline:
			t.Fatalf("timed out waiting for history+live events, got %+v", got)
		}
	}

	want := []progressEvent{{Step: "one"}, {Step: "two"}, {Step: "three"}}
	for i, evt := range want {
		if got[i] != evt {
			t.Errorf("event %d = %+v, want %+v", i, got[i], evt)
		}
	}
}

func TestBootStatus_SnapshotReflectsLatestState(t *testing.T) {
	bs := NewBootStatus()
	if step, done, err := bs.Snapshot(); step != "" || done || err != "" {
		t.Errorf("fresh snapshot = (%q,%v,%q), want empty", step, done, err)
	}

	bs.Step("warming")
	if step, _, _ := bs.Snapshot(); step != "warming" {
		t.Errorf("step after Step = %q, want warming", step)
	}

	bs.Err(errors.New("boom"))
	if _, _, errStr := bs.Snapshot(); errStr != "boom" {
		t.Errorf("err after Err = %q, want boom", errStr)
	}

	// Err does not flip done; Done does.
	if _, done, _ := bs.Snapshot(); done {
		t.Errorf("Err should not flip done")
	}
	bs.Done()
	if _, done, _ := bs.Snapshot(); !done {
		t.Errorf("Done did not flip done flag")
	}
}

func TestBootStatus_CancelRemovesSubscriber(t *testing.T) {
	bs := NewBootStatus()
	ch, cancel := bs.Subscribe()
	cancel()

	// After cancel the channel is closed; a second Step must not panic
	// (would happen if we still tried to send on the closed channel).
	bs.Step("after-cancel")

	// Reading from a closed channel yields the zero value + ok=false.
	if _, ok := <-ch; ok {
		t.Fatalf("channel should be closed after cancel")
	}

	// Calling cancel twice is idempotent (would double-close on a naive
	// implementation).
	cancel()
}

func TestBootStatus_SlowSubscriberGetsDropped(t *testing.T) {
	bs := NewBootStatus()
	ch, cancel := bs.Subscribe()
	defer cancel()

	// Fill the subscriber's buffer without reading. bootSubscriberBufSize
	// is 64, so emit 65 events — the 65th must overflow and close ch.
	for range bootSubscriberBufSize + 1 {
		bs.Step("fill")
	}

	// The channel must eventually close because the subscriber was
	// dropped. Drain anything still buffered, then assert close.
	drained := 0
	for {
		_, ok := <-ch
		if !ok {
			break
		}
		drained++
		if drained > bootSubscriberBufSize+10 {
			t.Fatalf("channel did not close after overflow, drained=%d", drained)
		}
	}
	if drained > bootSubscriberBufSize {
		t.Errorf("drained %d events, expected at most %d", drained, bootSubscriberBufSize)
	}
}

func TestBootStatus_NoEventsAfterDone(t *testing.T) {
	bs := NewBootStatus()
	ch, cancel := bs.Subscribe()
	defer cancel()

	bs.Done()
	bs.Step("after-done") // must be a no-op
	bs.Err(errors.New("late"))

	got := drain(ch, 50*time.Millisecond)
	if len(got) != 1 || !got[0].Done {
		t.Errorf("expected single Done event, got %+v", got)
	}
}

func TestBootStatus_ConcurrentPublishAndSubscribe(t *testing.T) {
	bs := NewBootStatus()

	var wg sync.WaitGroup
	// One publisher driving Step events.
	wg.Go(func() {
		for range 50 {
			bs.Step("step")
		}
		bs.Done()
	})

	// Multiple subscribers coming and going while the publisher runs.
	for range 10 {
		wg.Go(func() {
			ch, cancel := bs.Subscribe()
			defer cancel()
			for range ch {
				// drain; don't assert counts, just prove no deadlock.
			}
		})
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("concurrent publish/subscribe deadlocked")
	}
}
