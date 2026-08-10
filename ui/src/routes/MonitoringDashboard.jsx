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

// TABS is the single list both backends render from: the uPlot component
// to mount, and the uid of the Grafana dashboard that shows the same
// panels. The uids mirror monitoring.OverviewDashboardUID and
// monitoring.DNSDashboardUID in src/monitoring/dashboard.go — a uid that
// drifts is not an error anywhere, it is a "dashboard not found" page
// inside the iframe.
const TABS = [
  { value: 'system', label: 'System', grafanaUID: 'town-os-overview' },
  { value: 'dns', label: 'DNS', grafanaUID: 'town-os-dns' },
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

  // Port 5308 is exposed directly by the network controller — no proxy.
  const monitoringBase = getBaseURLForPort(5308)
  const grafanaUID = (TABS.find((t) => t.value === activeTab) || TABS[0]).grafanaUID
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
    return activeTab === 'dns' ? <DNSCharts /> : <MonitoringCharts diskDevices={status?.disk_devices || []} />
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
