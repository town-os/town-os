// Session-validity polling can be suspended for the duration of an
// operation that deliberately takes the system controller down.
//
// The poll in useRequireAuth exists to notice a session that has expired
// or been revoked and send the user back to the login page. During a
// Refresh Core Services that behaviour is actively wrong: the controller
// restarts with a freshly generated JWT signing key and clears the
// sessions table, so the moment the successor answers, every existing
// token is invalid and the poll's next tick navigates to "/". That
// unmounts the dialog rendering the restart — the operator is thrown to
// the login screen seconds after clicking the button and never sees a
// single stage of the update they asked for.
//
// So the refresh flow suspends the poll while it is watching the restart
// and only lets it resume once the dialog is done with it. The suspension
// is a module-level counter rather than React context because the thing
// being suspended (the interval in useRequireAuth) is mounted far above
// the thing doing the suspending (a dialog on one route), and nesting a
// provider between them buys nothing.
//
// suspendSessionChecks() returns the release function. It is idempotent:
// calling it twice releases once, so a caller can safely release on both
// an explicit close and an unmount cleanup.

let suspendCount = 0
/** @type {Set<() => void>} */
const listeners = new Set()

/** @returns {boolean} true while at least one caller holds a suspension. */
export function isSessionCheckSuspended() {
  return suspendCount > 0
}

/**
 * Suspend session-validity polling until the returned function is called.
 * @returns {() => void} release
 */
export function suspendSessionChecks() {
  suspendCount++
  notify()

  let released = false
  return () => {
    if (released) return
    released = true
    suspendCount = Math.max(0, suspendCount - 1)
    notify()
  }
}

/**
 * Subscribe to suspension changes. Shaped for useSyncExternalStore.
 * @param {() => void} listener
 * @returns {() => void} unsubscribe
 */
export function subscribeSessionChecks(listener) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** Test-only: drop every outstanding suspension. */
export function resetSessionChecks() {
  suspendCount = 0
  notify()
}

function notify() {
  for (const listener of listeners) listener()
}
