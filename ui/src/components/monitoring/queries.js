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
