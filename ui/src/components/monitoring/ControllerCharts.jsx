import UPlotChart from './UPlotChart.jsx'
import PanelBox from './PanelBox.jsx'
import {
  CONTROLLER_UNIT_STATE_QUERIES,
  CONTROLLER_UNIT_HEALTH_QUERIES,
  CONTROLLER_HTTP_QUERIES,
  CONTROLLER_AUDIT_QUERIES,
  CONTROLLER_FAILURE_QUERIES,
  CONTROLLER_PACKAGE_QUERIES,
  CONTROLLER_DISK_QUERIES,
  CONTROLLER_ACCOUNT_QUERIES,
  CONTROLLER_GRANTED_QUERIES,
  CONTROLLER_FILESYSTEM_QUERIES,
  CONTROLLER_UPTIME_QUERIES,
} from './queries.js'

// CONTROLLER_RANGE_SECONDS is 6h, matching the Grafana controller
// dashboard's default time range so the same panel shows the same window in
// either backend.
const CONTROLLER_RANGE_SECONDS = 21600

// Panel definitions are data rather than eleven hand-written JSX blocks, so
// the set stays in one place next to the Grafana panel list it mirrors. The
// titles are the Grafana titles verbatim: an operator who switches backends
// should not have to work out which panel became which.
const PANELS = [
  { title: 'Service Units by State', unit: 'short', queries: CONTROLLER_UNIT_STATE_QUERIES },
  { title: 'Service Health', unit: 'short', min: 0, max: 1, queries: CONTROLLER_UNIT_HEALTH_QUERIES },
  { title: 'API Requests by Status', unit: 'reqps', stacked: true, queries: CONTROLLER_HTTP_QUERIES },
  { title: 'Audit Events', unit: 'reqps', stacked: true, queries: CONTROLLER_AUDIT_QUERIES },
  { title: 'Recent Failures', unit: 'short', queries: CONTROLLER_FAILURE_QUERIES },
  { title: 'Package Inventory', unit: 'short', queries: CONTROLLER_PACKAGE_QUERIES },
  { title: 'Town OS Disk Usage', unit: 'bytes', stacked: true, queries: CONTROLLER_DISK_QUERIES },
  { title: 'Accounts', unit: 'short', stacked: true, queries: CONTROLLER_ACCOUNT_QUERIES },
  { title: 'Granted Accounts', unit: 'short', queries: CONTROLLER_GRANTED_QUERIES },
  { title: 'btrfs Subvolumes', unit: 'short', stacked: true, queries: CONTROLLER_FILESYSTEM_QUERIES },
  { title: 'Controller Uptime', unit: 's', queries: CONTROLLER_UPTIME_QUERIES },
]

/**
 * Eleven-panel dashboard for the system controller itself, the uPlot twin of
 * the Grafana "Town OS Controller" dashboard. Queries Prometheus on port
 * 5308 via the socat forwarder, exactly as MonitoringCharts does.
 *
 * Like the DNS tab and unlike the system one, this grid does NOT pin itself
 * to the viewport height: eleven panels squeezed into one screen leaves each
 * about 70px of canvas, at which point none of them is readable. Panels get
 * a fixed height and the page scrolls.
 */
export default function ControllerCharts() {
  return (
    <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
      {PANELS.map((p) => (
        <PanelBox key={p.title} className="h-72">
          <UPlotChart
            queries={p.queries}
            title={p.title}
            unit={p.unit}
            stacked={p.stacked}
            min={p.min}
            max={p.max}
            rangeSeconds={CONTROLLER_RANGE_SECONDS}
          />
        </PanelBox>
      ))}
    </div>
  )
}
