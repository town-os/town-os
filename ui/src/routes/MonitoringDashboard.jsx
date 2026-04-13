import { usePolling } from '@/lib/hooks.js'
import getClient, { getBaseURLForPort } from '@/lib/client-instance.js'
import {
  Card,
  CardContent,
} from '@/components/ui/card'
import { Activity, AlertCircle } from 'lucide-react'
import MonitoringCharts from '@/components/monitoring/MonitoringCharts.jsx'

export default function MonitoringDashboard() {
  const [status, , loading] = usePolling(
    () => getClient().monitoringStatus().catch(() => null),
    null,
    [],
    15000,
  )

  const backend = status?.backend || 'uplot'
  const isDisabled = status?.status === 'disabled'

  const coreRunning = status && status.prometheus && status.node_exporter
  const grafanaRunning = backend === 'grafana' && status?.grafana

  const allRunning = backend === 'grafana'
    ? coreRunning && grafanaRunning
    : coreRunning

  // Port 5308 is exposed directly by the network controller — no proxy.
  const monitoringBase = getBaseURLForPort(5308)
  const grafanaURL = monitoringBase + '/d/town-os-overview/town-os-overview?kiosk&theme=light'

  if (isDisabled) {
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground" style={{ height: 'calc(100vh - 104px)' }}>
        <Activity className="h-12 w-12 mb-4 opacity-30" />
        <p className="text-sm">Monitoring is not configured.</p>
      </div>
    )
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

      {backend === 'grafana' ? (
        <div className="rounded-lg border" style={{ height: 'calc(100vh - 104px)', minHeight: '500px' }}>
          {allRunning ? (
            <iframe
              src={grafanaURL}
              title="Grafana Dashboard"
              className="h-full w-full rounded border-0"
              sandbox="allow-same-origin allow-scripts allow-popups allow-forms"
            />
          ) : (
            <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
              <Activity className="h-12 w-12 mb-4 opacity-30" />
              <p className="text-sm">
                {loading
                  ? 'Checking monitoring status...'
                  : 'Monitoring services are starting up. The dashboard will appear once all services are running.'}
              </p>
            </div>
          )}
        </div>
      ) : (
        allRunning ? (
          <MonitoringCharts diskDevices={status?.disk_devices || []} />
        ) : (
          <div className="flex flex-col items-center justify-center text-muted-foreground" style={{ height: 'calc(100vh - 104px)' }}>
            <Activity className="h-12 w-12 mb-4 opacity-30" />
            <p className="text-sm">
              {loading
                ? 'Checking monitoring status...'
                : 'Monitoring services are starting up. Charts will appear once Prometheus is running.'}
            </p>
          </div>
        )
      )}
    </div>
  )
}
