import UPlotChart from './UPlotChart.jsx'

const RATE_INTERVAL = '5m'

const DISK_IO_QUERIES = [
  {
    expr: `rate(node_disk_read_bytes_total{device=~"sd.*|nvme.*|vd.*"}[${RATE_INTERVAL}]) * on(device) group_left node_filesystem_size_bytes{mountpoint="/town-os"} / node_filesystem_size_bytes{mountpoint="/town-os"}`,
    legend: 'Read',
  },
  {
    expr: `rate(node_disk_written_bytes_total{device=~"sd.*|nvme.*|vd.*"}[${RATE_INTERVAL}]) * on(device) group_left node_filesystem_size_bytes{mountpoint="/town-os"} / node_filesystem_size_bytes{mountpoint="/town-os"}`,
    legend: 'Write',
  },
]

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
 * Four-panel monitoring dashboard replicating the Grafana Town OS Overview.
 * Queries Prometheus on port 5308 via the socat forwarder.
 */
export default function MonitoringCharts() {
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2" style={{ height: 'calc(100vh - 104px)' }}>
      <div className="rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
        <UPlotChart
          queries={DISK_IO_QUERIES}
          title="Disk I/O (/town-os)"
          unit="Bps"
          rangeSeconds={21600}
        />
      </div>
      <div className="rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
        <UPlotChart
          queries={NETWORK_QUERIES}
          title="Network (External)"
          unit="bps"
          rangeSeconds={21600}
        />
      </div>
      <div className="rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
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
      <div className="rounded-lg border p-2" style={{ height: 'calc(50vh - 60px)', minHeight: '250px' }}>
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
