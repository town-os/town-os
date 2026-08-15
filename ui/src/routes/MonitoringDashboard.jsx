import { useSearchParams } from 'react-router-dom'
import { usePolling } from '@/lib/hooks.js'
import getClient, { getBaseURLForPort } from '@/lib/client-instance.js'
import {
  Card,
  CardContent,
} from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Activity, AlertCircle } from 'lucide-react'
import MonitoringCharts from '@/components/monitoring/MonitoringCharts.jsx'
import DNSCharts from '@/components/monitoring/DNSCharts.jsx'
import ControllerCharts from '@/components/monitoring/ControllerCharts.jsx'

// TABS is the single list both backends render from: the uPlot component
// to mount, and the uid of the Grafana dashboard that shows the same
// panels. The uids mirror monitoring.OverviewDashboardUID,
// monitoring.DNSDashboardUID and monitoring.ControllerDashboardUID in
// src/monitoring/dashboard.go — a uid that drifts is not an error anywhere,
// it is a "dashboard not found" page inside the iframe.
//
// The uPlot component is named here rather than chosen by a branch in
// renderBody: a tab whose branch was forgotten fell through to the system
// charts, which renders a real dashboard under the wrong tab heading — the
// failure mode that reads as working.
const TABS = [
  { value: 'system', label: 'System', grafanaUID: 'town-os-overview' },
  { value: 'dns', label: 'DNS', grafanaUID: 'town-os-dns', Charts: DNSCharts },
  { value: 'controller', label: 'Controller', grafanaUID: 'town-os-controller', Charts: ControllerCharts },
]

export default function MonitoringDashboard() {
  const [status, , loading] = usePolling(
    () => getClient().monitoringStatus().catch(() => null),
    null,
    [],
    60000,
  )

  // The tab lives in the URL, like every other sub-tabbed screen, so a
  // dashboard an operator is watching survives a reload and can be linked
  // to. An unknown ?tab= value falls back to system rather than rendering
  // nothing.
  const [searchParams, setSearchParams] = useSearchParams()
  const requested = searchParams.get('tab')
  const activeTab = TABS.some((t) => t.value === requested) ? requested : 'system'
  function setActiveTab(v) {
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev)
        p.set('tab', v)
        return p
      },
      { replace: true },
    )
  }

  const backend = status?.backend || 'uplot'
  const isDisabled = status?.status === 'disabled'

  const coreRunning = status && status.prometheus && status.node_exporter
  const grafanaRunning = backend === 'grafana' && status?.grafana

  const allRunning = backend === 'grafana'
    ? coreRunning && grafanaRunning
    : coreRunning

  // A failing scrape job is invisible from everywhere else on the box: every
  // unit is active, `systemctl --failed` is empty, and the panels that job
  // feeds draw an EMPTY chart rather than an error — which is what an idle
  // service looks like too. Prometheus's own target list is the only place the
  // difference exists, so it gets said out loud here, with the reason
  // Prometheus gave, rather than leaving the operator to guess from a flat
  // line. Both metrics bugs this box has shipped were exactly this shape.
  const downJobs = status?.down_jobs || []
  const downTargets = (status?.scrape_targets || []).filter((t) => t.health === 'down')
  // "Could not ask" is a different answer from "nothing is wrong", and
  // collapsing the two is the bug the target list exists to end.
  const targetsError = status?.scrape_targets_error

  // Port 5308 is exposed directly by the network controller — no proxy.
  const monitoringBase = getBaseURLForPort(5308)
  const tab = TABS.find((t) => t.value === activeTab) || TABS[0]
  const grafanaUID = tab.grafanaUID
  const grafanaURL = `${monitoringBase}/d/${grafanaUID}/${grafanaUID}?kiosk&theme=light&refresh=30s`

  if (isDisabled) {
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground" style={{ height: 'calc(100vh - 104px)' }}>
        <Activity className="h-12 w-12 mb-4 opacity-30" />
        <p className="text-sm">Monitoring is not configured.</p>
      </div>
    )
  }

  // Grafana's iframe fills the viewport; the uPlot DNS grid scrolls, so it
  // must not be boxed into a fixed-height container that would clip it.
  const startingUp = (
    <div className="flex flex-col items-center justify-center text-muted-foreground" style={{ height: 'calc(100vh - 104px)' }}>
      <Activity className="h-12 w-12 mb-4 opacity-30" />
      <p className="text-sm">
        {loading
          ? 'Checking monitoring status...'
          : backend === 'grafana'
            ? 'Monitoring services are starting up. The dashboard will appear once all services are running.'
            : 'Monitoring services are starting up. Charts will appear once Prometheus is running.'}
      </p>
    </div>
  )

  function renderBody() {
    if (!allRunning) return startingUp
    if (backend === 'grafana') {
      return (
        <div className="rounded-lg border" style={{ height: 'calc(100vh - 152px)', minHeight: '500px' }}>
          <iframe
            // Keyed on the uid so switching tabs replaces the frame rather
            // than navigating inside it: Grafana keeps its own history, and
            // a src swap on a live frame leaves the browser Back button
            // stepping through dashboards instead of leaving the page.
            key={grafanaUID}
            src={grafanaURL}
            title="Grafana Dashboard"
            className="h-full w-full rounded border-0"
            sandbox="allow-same-origin allow-scripts allow-popups allow-forms"
          />
        </div>
      )
    }
    // The system tab is the only one that needs a prop (the discovered
    // btrfs devices), so it stays the default rather than being wedged into
    // the table with a prop-passing indirection for one caller.
    const Charts = tab.Charts
    return Charts ? <Charts /> : <MonitoringCharts diskDevices={status?.disk_devices || []} />
  }

  return (
    <div className="space-y-4">
      {status && !allRunning && (
        <Card className="border-yellow-500/50 bg-yellow-50/50">
          <CardContent className="flex items-center gap-2 py-3">
            <AlertCircle className="h-4 w-4 text-yellow-600" />
            <span className="text-sm text-yellow-700">
              Some monitoring services are not running. Dashboards may be unavailable.
            </span>
          </CardContent>
        </Card>
      )}

      {downJobs.length > 0 && (
        <Card className="border-red-500/50 bg-red-50/50">
          <CardContent className="py-3">
            <div className="flex items-center gap-2">
              <AlertCircle className="h-4 w-4 text-red-600" />
              <span className="text-sm text-red-700">
                Prometheus cannot scrape {downJobs.join(', ')}. Charts fed by
                {downJobs.length === 1 ? ' that job' : ' those jobs'} will be empty, not wrong.
              </span>
            </div>
            <ul className="mt-2 space-y-1 pl-6 text-xs text-red-700/90">
              {downTargets.map((t) => (
                <li key={`${t.job}-${t.instance}`}>
                  <span className="font-medium">{t.job}</span>
                  {t.instance ? ` (${t.instance})` : ''}: {t.last_error || 'no reason reported'}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      {targetsError && (
        <Card className="border-yellow-500/50 bg-yellow-50/50">
          <CardContent className="flex items-center gap-2 py-3">
            <AlertCircle className="h-4 w-4 text-yellow-600" />
            <span className="text-sm text-yellow-700">
              Could not read Prometheus&apos;s target list: {targetsError}. Scrape failures cannot be reported until it answers.
            </span>
          </CardContent>
        </Card>
      )}

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value}>{t.label}</TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      {renderBody()}
    </div>
  )
}
