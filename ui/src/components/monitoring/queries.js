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
