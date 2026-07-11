package systemcontroller

import (
	"sync"

	"github.com/google/uuid"
)

// BootStatus tracks the current stage of systemcontroller startup and
// broadcasts step/done/error events to SSE subscribers. It exists so the UI
// can watch the boot proceed before :5309 is fully backed by the Echo
// router — the early HTTP handler binds immediately, exposes /boot-status
// streaming, then the full handler replaces it when startup finishes.
//
// Thread-safety: all methods are safe for concurrent use. Broadcast is
// non-blocking; a slow subscriber whose buffered channel fills up is
// dropped and closed, so the boot sequence is never stalled by a stuck
// consumer.
type BootStatus struct {
	// id identifies this process incarnation. It is regenerated on every
	// NewBootStatus (i.e. every systemcontroller start) and is reported
	// by both the boot stub's /status/ping and the full router's
	// /status/ping. A client that captured the id before asking for a
	// refresh can therefore tell "the old process is still answering"
	// (same id) from "the new process is up" (different id) — the two are
	// otherwise indistinguishable, since both serve a 200 ping and 404
	// the /boot-status route once booted.
	id string

	mu      sync.Mutex
	step    string
	done    bool
	err     error
	history []progressEvent
	subs    map[chan progressEvent]struct{}
}

// bootSubscriberBufSize is the per-subscriber channel buffer. Sized
// generously so normal subscribers never hit backpressure during a typical
// boot (the sequence emits ~25 events over several seconds); slow
// subscribers that do fill it get dropped rather than stalling the
// publisher.
const bootSubscriberBufSize = 64

// NewBootStatus returns a BootStatus with no subscribers and an empty
// history. The zero value is not valid because of the subs map.
func NewBootStatus() *BootStatus {
	return &BootStatus{
		id:   uuid.NewString(),
		subs: map[chan progressEvent]struct{}{},
	}
}

// BootID returns the identifier for this process incarnation. It is
// immutable for the life of the BootStatus, so no lock is taken.
func (b *BootStatus) BootID() string { return b.id }

// Step records a new step name as the current boot stage and broadcasts a
// progressEvent{Step} to every active subscriber.
func (b *BootStatus) Step(step string) {
	b.publish(progressEvent{Step: step}, func() { b.step = step })
}

// Err records an error as the terminal state and broadcasts it. After Err
// the boot is considered failed; Done should not normally follow.
func (b *BootStatus) Err(err error) {
	if err == nil {
		return
	}
	b.publish(progressEvent{Error: err.Error()}, func() { b.err = err })
}

// Done marks boot as finished and broadcasts the terminal Done event.
// Subsequent Step/Err calls are ignored so no event can follow Done on the
// wire.
func (b *BootStatus) Done() {
	b.publish(progressEvent{Done: true}, func() { b.done = true })
}

// Snapshot returns the current step, done flag, and error string (empty
// when no error). Used by the early /status/ping stub to answer without
// opening a full SSE subscription.
func (b *BootStatus) Snapshot() (step string, done bool, errStr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return b.step, b.done, b.err.Error()
	}
	return b.step, b.done, ""
}

// Subscribe registers a new listener and returns its receive channel plus
// a cancel closure. History is replayed into the channel first so late
// subscribers never miss earlier steps; after the replay the channel
// streams live events. The returned cancel removes and closes the channel
// — the SSE handler MUST defer it so client disconnect reclaims the
// subscriber slot.
//
// The channel buffer is sized to hold the full replay plus headroom for
// live events so the replay-under-lock step never blocks, even if boot
// has already emitted more events than bootSubscriberBufSize (e.g. a
// host with many installed packages amplifies the freshness stage).
func (b *BootStatus) Subscribe() (<-chan progressEvent, func()) {
	b.mu.Lock()
	bufSize := bootSubscriberBufSize
	if needed := len(b.history) + bootSubscriberBufSize; needed > bufSize {
		bufSize = needed
	}
	ch := make(chan progressEvent, bufSize)
	for _, evt := range b.history {
		ch <- evt
	}
	// If boot already finished, close the channel right after replay so
	// `for range ch` consumers exit without waiting for a Done event
	// that will never come. cancel() still works — it short-circuits on
	// the absent-subs check.
	if b.done {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; !ok {
			return
		}
		delete(b.subs, ch)
		close(ch)
	}
	return ch, cancel
}

// publish applies the state mutation under the lock, appends to history,
// and broadcasts to every subscriber. A subscriber whose buffer is full
// is dropped and closed — its client will notice the stream ending and
// reconnect, at which point history replay brings it back up to date.
func (b *BootStatus) publish(evt progressEvent, mutate func()) {
	b.mu.Lock()
	if b.done {
		// No events after Done. Prevents Err-after-Done or stray
		// late-step events from slipping past the terminal marker.
		b.mu.Unlock()
		return
	}
	mutate()
	b.history = append(b.history, evt)
	// Copy subscribers before unlocking so we don't hold the mutex while
	// sending. The non-blocking send semantic means the copy is cheap
	// (no retry loops, no goroutines spawned).
	dropped := make([]chan progressEvent, 0)
	subs := make([]chan progressEvent, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			dropped = append(dropped, ch)
		}
	}

	if len(dropped) > 0 {
		b.mu.Lock()
		for _, ch := range dropped {
			if _, ok := b.subs[ch]; ok {
				delete(b.subs, ch)
				close(ch)
			}
		}
		b.mu.Unlock()
	}

	// After Done, close every surviving subscriber so `for range ch`
	// readers exit cleanly. Late Subscribe() calls still get the full
	// history replay (including the Done event); this only affects
	// subscribers that were already connected when Done fired.
	if evt.Done {
		b.mu.Lock()
		for ch := range b.subs {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}
