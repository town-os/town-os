import { usePolling } from '@/lib/hooks.js'
import getClient, { getBaseURLForPort } from '@/lib/client-instance.js'
import {
  Card,
  CardContent,
} from '@/components/ui/card'
import { Activity, AlertCircle } from 'lucide-react'

export default function MonitoringDashboard() {
  const [status, , loading] = usePolling(
    () => getClient().monitoringStatus().catch(() => null),
    null,
    [],
    15000,
  )

  const [systemServices] = usePolling(
    () => getClient().listSystemServices().catch(() => []),
    [],
    [],
    15000,
  )

  const grafanaURL = status ? getBaseURLForPort(status.grafana.port) + '/d/town-os-overview/town-os-overview?kiosk&theme=light' : null

  const allRunning =
    status &&
    status.prometheus?.running &&
    status.node_exporter?.running &&
    status.grafana?.running

  const downServices = systemServices
    .filter((s) => s.ActiveState && s.ActiveState !== 'active')
    .map((s) => s.display_name)

  return (
    <div className="space-y-4">
      {status && !allRunning && (
        <Card className="border-yellow-500/50 bg-yellow-50/50">
          <CardContent className="flex items-center gap-2 py-3">
            <AlertCircle className="h-4 w-4 text-yellow-600" />
            <span className="text-sm text-yellow-700">
              {downServices.length > 0
                ? `${downServices.join(', ')} ${downServices.length === 1 ? 'is' : 'are'} not running. Dashboards may be unavailable.`
                : 'Some monitoring services are not running. Dashboards may be unavailable.'}
            </span>
          </CardContent>
        </Card>
      )}

      <div className="rounded-lg border" style={{ height: 'calc(100vh - 104px)', minHeight: '500px' }}>
        {allRunning && grafanaURL ? (
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
                : 'Monitoring services are starting up. The dashboard will appear once Grafana is running.'}
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
