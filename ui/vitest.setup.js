// Test setup: tolerate a known @radix-ui teardown quirk in jsdom.
//
// @radix-ui/react-focus-scope restores focus on unmount via a setTimeout(0)
// that dispatches an event. Under jsdom that argument can fail Event validation
// and EventTarget.dispatchEvent throws "parameter 1 is not of type Event"
// *after* the component has unmounted — a harmless focus restoration on a
// torn-down tree, but vitest reports it as an unhandled error and fails the run.
// Guard against a non-Event argument so the teardown no-ops instead of throwing.
// Real tests always dispatch real Event instances, so this never masks a genuine
// failure.
const originalDispatchEvent = EventTarget.prototype.dispatchEvent
EventTarget.prototype.dispatchEvent = function dispatchEvent(event) {
  if (!(event instanceof Event)) return false
  return originalDispatchEvent.call(this, event)
}
