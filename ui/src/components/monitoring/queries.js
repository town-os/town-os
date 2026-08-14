// Query builders for the monitoring dashboard. Kept in a separate,
// non-component file so MonitoringCharts.jsx only exports React
// components and satisfies react-refresh/only-export-components.

export const RATE_INTERVAL = '5m'

// NO_BTRFS_DEVICES_SENTINEL mirrors monitoring.NoBtrfsDevicesSentinel in
// the Go code: a label value no real kernel device can match, so the
// Disk I/O panel renders empty rather than silently summing every disk
// on the host when device discovery failed.
export const NO_BTRFS_DEVICES_SENTINEL = '__no_btrfs_devices__'

// NETWORK_DEVICE_EXCLUDE drops virtual / container-local interfaces.
// Mirrored verbatim in src/monitoring/config.go.
export const NETWORK_DEVICE_EXCLUDE =
  'lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*'

/**
 * Build the Disk I/O queries from the list of kernel device basenames
 * backing the btrfs filesystem at /town-os. The result is a sum across
 * those devices so the panel shows one Read line and one Write line
 * regardless of how many physical devices the filesystem spans.
 */
export function buildDiskIOQueries(diskDevices) {
  const regex = diskDevices && diskDevices.length > 0
    ? diskDevices.join('|')
    : NO_BTRFS_DEVICES_SENTINEL
  return [
    {
      expr: `sum(rate(node_disk_read_bytes_total{device=~"${regex}"}[${RATE_INTERVAL}]))`,
      legend: 'Read',
    },
    {
      expr: `sum(rate(node_disk_written_bytes_total{device=~"${regex}"}[${RATE_INTERVAL}]))`,
      legend: 'Write',
    },
  ]
}

// NETWORK_QUERIES restricts to physical interfaces (excluding the virtual
// device list above) AND keeps only series whose interface is currently up
// via `node_network_up == 1`. Without the up-state join, host interfaces
// that have ever existed but are now down still appear as flat zero lines
// in the legend, which on a multi-NIC box runs the labels off-screen.
export const NETWORK_QUERIES = [
  {
    expr: `(rate(node_network_receive_bytes_total{device!~"${NETWORK_DEVICE_EXCLUDE}"}[${RATE_INTERVAL}]) and on (device) (node_network_up == 1)) * 8`,
    legend: '{{device}} Rx',
  },
  {
    expr: `(rate(node_network_transmit_bytes_total{device!~"${NETWORK_DEVICE_EXCLUDE}"}[${RATE_INTERVAL}]) and on (device) (node_network_up == 1)) * 8`,
    legend: '{{device}} Tx',
  },
]

// --- rolodex (DNS) ---------------------------------------------------
//
// Every query below is the uPlot twin of a panel in the Grafana DNS
// dashboard (src/monitoring/dashboard_dns.go), and the two must stay
// identical apart from the rate window: Grafana expands its rate-interval
// macro per panel, and this frontend has no macro expansion, so it pins
// RATE_INTERVAL instead. A Go test
// (TestRolodexDashboardMirroredInFrontendQueries) fails if either side
// names a rolodex metric the other does not — and it also rejects the
// macro's literal spelling anywhere in this file, which is why it is
// described rather than quoted here.

// ROLODEX_SELECTOR mirrors monitoring.RolodexJobName. Selecting on the job
// rather than relying on the metric prefix is what keeps these panels
// pointed at the box's own resolver if a second DNS exporter ever lands in
// the same Prometheus.
export const ROLODEX_SELECTOR = '{job="rolodex"}'

const rate = (metric) => `rate(${metric}${ROLODEX_SELECTOR}[${RATE_INTERVAL}])`
const sumBy = (label, metric) => `sum by (${label}) (${rate(metric)})`
const sumAll = (metric) => `sum(${rate(metric)})`
// Summing by le before histogram_quantile is mandatory: the raw bucket
// series carry a proto label, and quantiling them unaggregated draws one
// line per transport instead of the box-wide latency the panel is titled
// for.
const quantile = (q, metric) =>
  `histogram_quantile(${q}, sum by (le) (${rate(metric + '_bucket')}))`

export const DNS_QUERY_RCODE_QUERIES = [
  { expr: sumBy('rcode', 'rolodex_dns_queries_total'), legend: '{{rcode}}' },
]

export const DNS_LATENCY_QUERIES = [
  { expr: quantile(0.5, 'rolodex_dns_query_duration_seconds'), legend: 'p50' },
  { expr: quantile(0.95, 'rolodex_dns_query_duration_seconds'), legend: 'p95' },
  { expr: quantile(0.99, 'rolodex_dns_query_duration_seconds'), legend: 'p99' },
]

export const DNS_ANSWER_SOURCE_QUERIES = [
  { expr: sumBy('source', 'rolodex_dns_answers_total'), legend: '{{source}}' },
]

// The denominator is deliberately unclamped: an idle box divides zero by
// zero and the line breaks, which is honest. Clamping would draw a
// confident 0% hit ratio for a cache nothing has asked anything.
const cacheHits = sumAll('rolodex_dns_cache_hits_total')
const cacheNegativeHits = sumAll('rolodex_dns_cache_negative_hits_total')
const cacheMisses = sumAll('rolodex_dns_cache_misses_total')

export const DNS_CACHE_RATIO_QUERIES = [
  {
    expr: `100 * (${cacheHits} + ${cacheNegativeHits}) / (${cacheHits} + ${cacheNegativeHits} + ${cacheMisses})`,
    legend: 'Hit ratio',
  },
]

export const DNS_CACHE_ENTRY_QUERIES = [
  { expr: `rolodex_dns_cache_entries${ROLODEX_SELECTOR}`, legend: 'Positive' },
  { expr: `rolodex_dns_cache_negative_entries${ROLODEX_SELECTOR}`, legend: 'Negative' },
  { expr: `rolodex_dns_blocklist_cache_entries${ROLODEX_SELECTOR}`, legend: 'Blocklist' },
]

// Refusals sit next to blocks on purpose: a provider answering "stop
// asking" rather than "listed" is what silently turns a blocklist into an
// outage, and it only reads as anomalous beside the block rate it replaced.
export const DNS_BLOCKLIST_QUERIES = [
  { expr: sumBy('kind', 'rolodex_dns_blocklist_blocks_total'), legend: 'blocked {{kind}}' },
  { expr: sumAll('rolodex_dns_blocklist_allowlisted_total'), legend: 'allowlisted' },
  { expr: sumAll('rolodex_dns_blocklist_refusals_total'), legend: 'refused' },
]

export const DNS_UPSTREAM_TIER_QUERIES = [
  { expr: sumBy('tier', 'rolodex_dns_upstream_tier_wins_total'), legend: '{{tier}} wins' },
  { expr: sumBy('tier', 'rolodex_dns_upstream_tier_failures_total'), legend: '{{tier}} failures' },
  { expr: sumAll('rolodex_dns_upstream_exhausted_total'), legend: 'exhausted' },
]

export const DNS_TRAFFIC_QUERIES = [
  { expr: sumBy('direction', 'rolodex_dns_traffic_bytes_total'), legend: '{{direction}}' },
]

// --- system controller ------------------------------------------------
//
// The uPlot twin of the Grafana controller dashboard
// (src/monitoring/dashboard_controller.go), under the same rule as the DNS
// block above: identical apart from the rate window, and guarded by
// TestControllerDashboardMirroredInFrontendQueries, which fails if either
// side names a controller metric family the other does not.
//
// That guard scans this file for the metric prefix, so the prefix is
// described rather than written out in prose here — a bare mention in a
// comment would read as a nineteenth family that no panel queries.

// CONTROLLER_SELECTOR mirrors monitoring.ControllerJobName. The controller
// is the only thing exporting these families today, but the selector is
// what keeps these panels pointed at this box's controller rather than at
// whatever else lands in the same Prometheus.
export const CONTROLLER_SELECTOR = '{job="systemcontroller"}'

const gauge = (metric) => `${metric}${CONTROLLER_SELECTOR}`
const controllerRate = (metric) =>
  `rate(${metric}${CONTROLLER_SELECTOR}[${RATE_INTERVAL}])`
// Summing away the labels the panel does not legend on:
// townos_http_requests_total carries method as well as status, and a status
// panel that kept method would draw a line per pair.
const controllerSumBy = (label, metric) =>
  `sum by (${label}) (${controllerRate(metric)})`
// An average out of a pair of counters: a sum of observations over the count
// of them. Both sides aggregate by the same label, or the division matches on
// the full label set and silently drops series.
const controllerRatio = (label, numerator, denominator) =>
  `sum by (${label}) (${controllerRate(numerator)}) / sum by (${label}) (${controllerRate(denominator)})`
// Deliberately unclamped: a zero denominator is a filesystem the collector
// could not read, and the break in the line is honest where a clamp would draw
// a confident 0% full.
const controllerPercent = (part, whole) =>
  `100 * ${gauge(part)} / ${gauge(whole)}`

export const CONTROLLER_UNIT_STATE_QUERIES = [
  { expr: gauge('townos_system_units'), legend: 'system {{state}}' },
  { expr: gauge('townos_package_units'), legend: 'package {{state}}' },
]

export const CONTROLLER_UNIT_HEALTH_QUERIES = [
  { expr: gauge('townos_system_unit_active'), legend: '{{unit}}' },
  { expr: gauge('townos_package_unit_active'), legend: '{{unit}}' },
]

export const CONTROLLER_HTTP_QUERIES = [
  { expr: controllerSumBy('status', 'townos_http_requests_total'), legend: '{{status}}' },
]

// Mean seconds per request, which is what separates "the box is busy" from
// "the box is stuck" — the request-rate panel answers identically whether each
// call takes 5ms or 5s.
export const CONTROLLER_LATENCY_QUERIES = [
  {
    expr: controllerRatio('method', 'townos_http_request_seconds_total', 'townos_http_requests_total'),
    legend: '{{method}}',
  },
]

export const CONTROLLER_AUDIT_QUERIES = [
  { expr: controllerSumBy('result', 'townos_audit_events_total'), legend: '{{result}}' },
]

export const CONTROLLER_FAILURE_QUERIES = [
  { expr: gauge('townos_audit_recent_errors'), legend: 'Audit failures (5m)' },
  { expr: gauge('townos_repository_errors'), legend: 'Repository refresh errors' },
]

// One inventory panel, without the catalogue size: these are all counts in the
// tens, so they share an axis legibly.
export const CONTROLLER_INVENTORY_QUERIES = [
  { expr: gauge('townos_packages_installed'), legend: 'Installed packages' },
  { expr: gauge('townos_upgrades_available'), legend: 'Upgradable' },
  { expr: gauge('townos_repositories'), legend: 'Repositories' },
  { expr: gauge('townos_filesystems'), legend: '{{state}} subvolumes' },
]

// Used and available stack to the filesystem size, so the panel shows the
// fill and the headroom without a third series restating either.
export const CONTROLLER_DISK_QUERIES = [
  { expr: gauge('townos_disk_used_bytes'), legend: 'Used' },
  { expr: gauge('townos_disk_available_bytes'), legend: 'Available' },
]

// The same disk as a percentage of its size, on a pinned 0-100 axis: the bytes
// panel cannot answer "how close is this to full" without arithmetic against
// an axis whose scale depends on the box.
export const CONTROLLER_DISK_FILL_QUERIES = [
  { expr: controllerPercent('townos_disk_used_bytes', 'townos_disk_total_bytes'), legend: 'Used' },
]

// Per core-second, so a controller genuinely using two cores reads 200. The
// axis is deliberately not capped at 100.
export const CONTROLLER_CPU_QUERIES = [
  { expr: `100 * ${controllerRate('townos_process_cpu_seconds_total')}`, legend: 'CPU' },
]

// The gap between the two is the diagnosis: heap climbing means the controller
// is holding objects it should have dropped, RSS climbing over a flat heap
// means the memory went somewhere the Go allocator does not account for.
export const CONTROLLER_MEMORY_QUERIES = [
  { expr: gauge('townos_memory_heap_bytes'), legend: 'Heap' },
  { expr: gauge('townos_memory_rss_bytes'), legend: 'Resident' },
]

// Three counts that are flat on a healthy box and climb without bound on a
// leaking one — a handler that never returns holds a goroutine, a descriptor,
// and an in-flight request each.
export const CONTROLLER_CONCURRENCY_QUERIES = [
  { expr: gauge('townos_goroutines'), legend: 'Goroutines' },
  { expr: gauge('townos_open_files'), legend: 'Open files' },
  { expr: gauge('townos_http_requests_in_flight'), legend: 'Requests in flight' },
]

// Unstacked: the kinds partition the account list, but the grant count is a
// subset of the user bucket, and stacking it would draw a total larger than
// the number of accounts that exist.
export const CONTROLLER_ACCOUNT_QUERIES = [
  { expr: gauge('townos_accounts'), legend: '{{kind}}' },
  { expr: gauge('townos_accounts_granted'), legend: 'holding a grant' },
]

// The sawtooth is the signal, not the height: a controller quietly
// crash-looping under Restart=always looks healthy on every other panel.
export const CONTROLLER_UPTIME_QUERIES = [
  { expr: `time() - ${gauge('townos_start_time_seconds')}`, legend: 'Uptime' },
]
