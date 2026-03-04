import { usePolling } from '@/lib/hooks.js'
import getClient, { getBaseURLForPort } from '@/lib/client-instance.js'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Activity, AlertCircle, Loader2 } from 'lucide-react'

function StatusBadge({ running }) {
  return running ? (
    <Badge variant="default" className="bg-green-600">
      Running
    </Badge>
  ) : (
    <Badge variant="destructive">Stopped</Badge>
  )
}

export default function MonitoringDashboard() {
  const [status, , loading] = usePolling(
    () => getClient().monitoringStatus().catch(() => null),
    null,
    [],
    15000,
  )

  const grafanaURL = status ? getBaseURLForPort(status.grafana.port) + '/' : null

  const allRunning =
    status &&
    status.prometheus?.running &&
    status.node_exporter?.running &&
    status.grafana?.running

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Monitoring</h1>
        <p className="text-muted-foreground">
          System metrics and dashboards powered by Prometheus and Grafana.
        </p>
      </div>

      <div className="grid gap-4 grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Prometheus</CardTitle>
            <CardDescription>Metrics collection</CardDescription>
          </CardHeader>
          <CardContent>
            {loading && !status ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : status ? (
              <StatusBadge running={status.prometheus?.running} />
            ) : (
              <Badge variant="outline">Unknown</Badge>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">
              Node Exporter
            </CardTitle>
            <CardDescription>System metrics</CardDescription>
          </CardHeader>
          <CardContent>
            {loading && !status ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : status ? (
              <StatusBadge running={status.node_exporter?.running} />
            ) : (
              <Badge variant="outline">Unknown</Badge>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Grafana</CardTitle>
            <CardDescription>Dashboards</CardDescription>
          </CardHeader>
          <CardContent>
            {loading && !status ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : status ? (
              <StatusBadge running={status.grafana?.running} />
            ) : (
              <Badge variant="outline">Unknown</Badge>
            )}
          </CardContent>
        </Card>
      </div>

      {status && !allRunning && (
        <Card className="border-yellow-500/50 bg-yellow-50/50">
          <CardContent className="flex items-center gap-2 py-3">
            <AlertCircle className="h-4 w-4 text-yellow-600" />
            <span className="text-sm text-yellow-700">
              Some monitoring services are not running. Dashboards may be
              unavailable.
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
