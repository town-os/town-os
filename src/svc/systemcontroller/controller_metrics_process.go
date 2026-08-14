package systemcontroller

import (
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"gitea.com/town-os/town-os/src/metrics"
)

// procStatm is the kernel's per-process memory summary. Its second field is the
// resident set in pages, which is the number an operator means by "how much
// memory is this using" — Go's own heap figures describe what the runtime has
// allocated, not what the kernel is holding for the process.
const procStatm = "/proc/self/statm"

// procFDDir holds one entry per open file descriptor. Counting the entries is
// the only portable-enough way to see a descriptor leak: nothing in the Go
// runtime tracks them, and a controller that leaks one per request dies of
// EMFILE hours later with no warning on any other panel.
const procFDDir = "/proc/self/fd"

// collectProcessMetrics reports the health of the controller process itself:
// how much memory it holds, how much CPU it has burned, how many goroutines and
// descriptors it is carrying.
//
// This is the section that answers "why is the box slow" rather than "what is
// the box running". Everything else the controller exports describes what it
// manages — units, packages, accounts, disk — and all of it looks perfectly
// healthy while the controller leaks goroutines into swap. node-exporter cannot
// close the gap either: it reports the host, and on a box whose whole job is
// running containers, the controller's own share of that is invisible in the
// host totals.
//
// Every reading is independent and omitted on error, for the same reason the
// manager sections are: a scrape that fails as a unit is useless in the
// situation it exists for.
func collectProcessMetrics() []metrics.Metric {
	// ReadMemStats stops the world for the duration of the read. That is
	// microseconds on a heap this size, once per scrape interval, and it is the
	// only way to get the figures without a dependency; the alternative shape —
	// caching them from a background goroutine — would report memory as of some
	// unrelated moment, which is exactly the property that makes a leak hard to
	// see.
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	out := []metrics.Metric{
		metrics.Gauge("townos_goroutines",
			"Goroutines currently running in the system controller.",
			float64(runtime.NumGoroutine())),
		// HeapAlloc rather than HeapSys: the bytes actually held by live
		// objects, which is what climbs when something is leaked. HeapSys
		// includes memory the runtime has kept from the OS after a collection
		// and would show a sawtooth flattening into a plateau that means
		// nothing.
		metrics.Gauge("townos_memory_heap_bytes",
			"Bytes of live heap objects in the system controller.",
			float64(mem.HeapAlloc)),
	}

	if rss, ok := processRSSBytes(); ok {
		out = append(out, metrics.Gauge("townos_memory_rss_bytes",
			"Resident set size of the system controller process.", rss))
	}
	if fds, ok := openFileCount(); ok {
		out = append(out, metrics.Gauge("townos_open_files",
			"File descriptors held open by the system controller.", fds))
	}
	if cpu, ok := processCPUSeconds(); ok {
		out = append(out, metrics.Counter("townos_process_cpu_seconds_total",
			"CPU seconds consumed by the system controller since it started.", cpu))
	}

	return out
}

// processRSSBytes reads the resident set size from /proc/self/statm, in bytes.
//
// The field is a page count, so it is multiplied by the page size rather than
// assumed to be 4096: this ships on aarch64 as well as x86_64, and a kernel
// built with 16K pages would otherwise under-report memory by a factor of four
// — the kind of wrong that still looks plausible on a graph.
func processRSSBytes() (float64, bool) {
	raw, err := os.ReadFile(procStatm)
	if err != nil {
		slog.Debug("metrics: reading process memory", "path", procStatm, "error", err)
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		slog.Debug("metrics: unexpected statm layout", "path", procStatm, "fields", len(fields))
		return 0, false
	}
	pages, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		slog.Debug("metrics: parsing resident pages", "value", fields[1], "error", err)
		return 0, false
	}
	return pages * float64(os.Getpagesize()), true
}

// openFileCount counts the entries in /proc/self/fd.
//
// The directory handle this read opens is itself one of the descriptors it
// counts, and it is not subtracted: the reading is consistently one high, which
// is invisible against the trend the panel is for and cheaper to explain than a
// correction that would be wrong if the runtime ever read the directory
// differently.
func openFileCount() (float64, bool) {
	entries, err := os.ReadDir(procFDDir)
	if err != nil {
		slog.Debug("metrics: counting open descriptors", "path", procFDDir, "error", err)
		return 0, false
	}
	return float64(len(entries)), true
}

// processCPUSeconds returns user plus system CPU time for this process.
//
// Taken from getrusage rather than parsed out of /proc/self/stat: it is one
// syscall with no field-offset assumptions, and the two halves are added
// because a panel that separated them would be asking an operator to care
// which side of a syscall boundary the time was spent on.
func processCPUSeconds() (float64, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		slog.Debug("metrics: reading process cpu time", "error", err)
		return 0, false
	}
	return timevalSeconds(ru.Utime) + timevalSeconds(ru.Stime), true
}

// timevalSeconds renders a rusage timeval as fractional seconds. The fields are
// int64 on amd64 and int32 on some 32-bit targets, so both are converted
// explicitly rather than relying on the platform's width.
func timevalSeconds(tv syscall.Timeval) float64 {
	return float64(tv.Sec) + float64(tv.Usec)/1e6
}
