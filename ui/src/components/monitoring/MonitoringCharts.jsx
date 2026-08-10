import { useMemo } from 'react'
import UPlotChart from './UPlotChart.jsx'
import PanelBox from './PanelBox.jsx'
import { RATE_INTERVAL, buildDiskIOQueries, NETWORK_QUERIES } from './queries.js'

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
 *
 * The grid uses explicit grid-rows so each cell collapses to half the
 * available viewport height (with min-h-0 so the children can shrink),
 * keeping all four panels — chart, axes, and legend — inside a 1080p
 * viewport without horizontal or vertical overflow.
 */
export default function MonitoringCharts({ diskDevices }) {
  const diskIOQueries = useMemo(() => buildDiskIOQueries(diskDevices), [diskDevices])

  return (
    <div
      className="grid grid-cols-1 grid-rows-4 gap-2 lg:grid-cols-2 lg:grid-rows-2"
      // 152px, not 96: the dashboard now carries a tab bar above this grid,
      // and the old figure pushed the bottom row's legend off-screen.
      style={{ height: 'calc(100vh - 152px)' }}
    >
      <PanelBox>
        <UPlotChart
          queries={diskIOQueries}
          title="Disk I/O (/town-os)"
          unit="Bps"
          rangeSeconds={21600}
        />
      </PanelBox>
      <PanelBox>
        <UPlotChart
          queries={NETWORK_QUERIES}
          title="Network (External)"
          unit="bps"
          rangeSeconds={21600}
        />
      </PanelBox>
      <PanelBox>
        <UPlotChart
          queries={CPU_QUERIES}
          title="CPU Usage"
          unit="percent"
          stacked
          min={0}
          max={100}
          rangeSeconds={21600}
        />
      </PanelBox>
      <PanelBox>
        <UPlotChart
          queries={MEMORY_QUERIES}
          title="Memory Usage"
          unit="bytes"
          rangeSeconds={21600}
        />
      </PanelBox>
    </div>
  )
}
