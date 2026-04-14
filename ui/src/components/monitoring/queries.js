// Query builders for the monitoring dashboard. Kept in a separate,
// non-component file so MonitoringCharts.jsx only exports React
// components and satisfies react-refresh/only-export-components.

export const RATE_INTERVAL = '5m'

// NO_BTRFS_DEVICES_SENTINEL mirrors monitoring.NoBtrfsDevicesSentinel in
// the Go code: a label value no real kernel device can match, so the
// Disk I/O panel renders empty rather than silently summing every disk
// on the host when device discovery failed.
export const NO_BTRFS_DEVICES_SENTINEL = '__no_btrfs_devices__'

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
