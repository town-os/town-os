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

// Provide Web Storage when the runtime doesn't. jsdom on older Node supplies a
// working localStorage/sessionStorage, but Node 24+ ships its own built-in
// Web Storage global that is `undefined` unless `--localstorage-file` is passed,
// and that missing global shadows jsdom's. Gate on the feature's existence: when
// the runtime already provides Web Storage we leave it untouched, and only when
// it's absent do we install a Map-backed stub — so tests pass on both old and
// new Node without depending on the flag.
function installStorage(name) {
  if (globalThis[name]) return
  const store = new Map()
  const storage = {
    getItem: (k) => (store.has(String(k)) ? store.get(String(k)) : null),
    setItem: (k, v) => { store.set(String(k), String(v)) },
    removeItem: (k) => { store.delete(String(k)) },
    clear: () => { store.clear() },
    key: (i) => Array.from(store.keys())[i] ?? null,
    get length() { return store.size },
  }
  Object.defineProperty(globalThis, name, {
    configurable: true,
    writable: true,
    value: storage,
  })
}
installStorage('localStorage')
installStorage('sessionStorage')
