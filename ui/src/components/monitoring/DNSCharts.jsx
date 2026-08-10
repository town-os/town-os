import UPlotChart from './UPlotChart.jsx'
import PanelBox from './PanelBox.jsx'
import {
  DNS_QUERY_RCODE_QUERIES,
  DNS_LATENCY_QUERIES,
  DNS_ANSWER_SOURCE_QUERIES,
  DNS_CACHE_RATIO_QUERIES,
  DNS_CACHE_ENTRY_QUERIES,
  DNS_BLOCKLIST_QUERIES,
  DNS_UPSTREAM_TIER_QUERIES,
  DNS_TRAFFIC_QUERIES,
} from './queries.js'

// DNS_RANGE_SECONDS is 6h, matching the Grafana DNS dashboard's default
// time range so the same panel shows the same window in either backend.
const DNS_RANGE_SECONDS = 21600

// Panel definitions are data rather than eight hand-written JSX blocks, so
// the set stays in one place next to the Grafana panel list it mirrors.
// The titles are the Grafana titles verbatim: an operator who switches
// backends should not have to work out which panel became which.
const PANELS = [
  { title: 'DNS Queries by Response Code', unit: 'reqps', stacked: true, queries: DNS_QUERY_RCODE_QUERIES },
  { title: 'Query Latency', unit: 's', queries: DNS_LATENCY_QUERIES },
  { title: 'Answers by Source', unit: 'reqps', stacked: true, queries: DNS_ANSWER_SOURCE_QUERIES },
  { title: 'Cache Hit Ratio', unit: 'percent', min: 0, max: 100, queries: DNS_CACHE_RATIO_QUERIES },
  { title: 'Cache Entries', unit: 'short', queries: DNS_CACHE_ENTRY_QUERIES },
  { title: 'Blocklist Activity', unit: 'reqps', queries: DNS_BLOCKLIST_QUERIES },
  { title: 'Upstream Tier Outcomes', unit: 'reqps', queries: DNS_UPSTREAM_TIER_QUERIES },
  { title: 'DNS Traffic', unit: 'Bps', queries: DNS_TRAFFIC_QUERIES },
]

/**
 * Eight-panel DNS dashboard for rolodex, the uPlot twin of the Grafana
 * "Town OS DNS" dashboard. Queries Prometheus on port 5308 via the socat
 * forwarder, exactly as MonitoringCharts does.
 *
 * Unlike the system tab, this grid does NOT pin itself to the viewport
 * height: eight panels squeezed into one screen leaves each about 100px of
 * canvas, at which point a latency chart is decoration. Panels get a fixed
 * height and the page scrolls.
 */
export default function DNSCharts() {
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
            rangeSeconds={DNS_RANGE_SECONDS}
          />
        </PanelBox>
      ))}
    </div>
  )
}
