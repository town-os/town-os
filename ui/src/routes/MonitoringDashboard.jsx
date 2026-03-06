import { usePolling } from '@/lib/hooks.js'
import getClient, { getBaseURLForPort } from '@/lib/client-instance.js'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
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

  const grafanaURL = status ? getBaseURLForPort(status.grafana.port) + '/d/town-os-overview/town-os-overview?kiosk' : null

  const allRunning =
    status &&
    status.prometheus?.running &&
    status.node_exporter?.running &&
    status.grafana?.running

  const downServices = systemServices
    .filter((s) => s.ActiveState && s.ActiveState !== 'active')
    .map((s) => s.display_name)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Monitoring</h1>
        <p className="text-muted-foreground">
          System metrics and dashboards powered by Prometheus and Grafana.
        </p>
      </div>

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

      <Card className="overflow-hidden">
        <CardHeader className="pb-2">
          <CardTitle className="flex items-center gap-2">
            <Activity className="h-5 w-5" />
            System Dashboard
          </CardTitle>
          <CardDescription>
            Live system metrics from Grafana. Use the panels to explore CPU,
            memory, disk, and network usage.
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {allRunning && grafanaURL ? (
            <iframe
              src={grafanaURL}
              title="Grafana Dashboard"
              className="w-full border-0"
              style={{ height: 'calc(100vh - 320px)', minHeight: '500px' }}
              sandbox="allow-same-origin allow-scripts allow-popups allow-forms"
            />
          ) : (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
              <Activity className="h-12 w-12 mb-4 opacity-30" />
              <p className="text-sm">
                {loading
                  ? 'Checking monitoring status...'
                  : 'Monitoring services are starting up. The dashboard will appear once Grafana is running.'}
              </p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
