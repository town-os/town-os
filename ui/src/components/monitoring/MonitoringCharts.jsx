import { useMemo } from 'react'
import UPlotChart from './UPlotChart.jsx'

const RATE_INTERVAL = '5m'

// NO_BTRFS_DEVICES_SENTINEL mirrors monitoring.NoBtrfsDevicesSentinel in
// the Go code: a label value no real kernel device can match, so the
// Disk I/O panel renders empty rather than silently summing every disk
// on the host when device discovery failed.
const NO_BTRFS_DEVICES_SENTINEL = '__no_btrfs_devices__'

const NETWORK_QUERIES = [
  {
    expr: `rate(node_network_receive_bytes_total{device!~"lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*"}[${RATE_INTERVAL}]) * 8`,
    legend: '{{device}} Rx',
  },
  {
    expr: `rate(node_network_transmit_bytes_total{device!~"lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*"}[${RATE_INTERVAL}]) * 8`,
    legend: '{{device}} Tx',
  },
]

const CPU_QUERIES = [
  {
    expr: `sum by (mode) (rate(node_cpu_seconds_total{mode=~"user|system|iowait|irq|softirq|steal|nice"}[${RATE_INTERVAL}])) * 100 / scalar(count(node_cpu_seconds_total{mode="user"}))`,
    legend: '{{mode}}',
  },
  {
    expr: `(1 - sum(rate(node_cpu_seconds_total{mode="idle"}[${RATE_INTERVAL}])) / scalar(count(node_cpu_seconds_total{mode="user"}))) * 100`,
    legend: 'Total',
  },
]

const MEMORY_QUERIES = [
  { expr: 'node_memory_MemTotal_bytes', legend: 'Total' },
  { expr: 'node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes', legend: 'Used' },
  { expr: 'node_memory_MemAvailable_bytes', legend: 'Available' },
]

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

/**
 * Four-panel monitoring dashboard replicating the Grafana Town OS Overview.
 * Queries Prometheus on port 5308 via the socat forwarder.
 */
export default function MonitoringCharts({ diskDevices }) {
  const diskIOQueries = useMemo(() => buildDiskIOQueries(diskDevices), [diskDevices])

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2" style={{ height: 'calc(100vh - 104px)' }}>
      <div className="overflow-hidden rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
        <UPlotChart
          queries={diskIOQueries}
          title="Disk I/O (/town-os)"
          unit="Bps"
          rangeSeconds={21600}
        />
      </div>
      <div className="overflow-hidden rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
        <UPlotChart
          queries={NETWORK_QUERIES}
          title="Network (External)"
          unit="bps"
          rangeSeconds={21600}
        />
      </div>
      <div className="overflow-hidden rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
        <UPlotChart
          queries={CPU_QUERIES}
          title="CPU Usage"
          unit="percent"
          stacked
          min={0}
          max={100}
          rangeSeconds={21600}
        />
      </div>
      <div className="overflow-hidden rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
        <UPlotChart
          queries={MEMORY_QUERIES}
          title="Memory Usage"
          unit="bytes"
          rangeSeconds={21600}
        />
      </div>
    </div>
  )
}
