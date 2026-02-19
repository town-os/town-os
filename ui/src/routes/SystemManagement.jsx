import { useState } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { MoreHorizontal, Play, Square, RotateCcw, Check, X } from 'lucide-react'

export default function SystemManagement() {
  const [error, setError] = useState(null)
  const [success, setSuccess] = useState(null)
  const [actionConfirm, setActionConfirm] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('Name')
  const [sortDirection, setSortDirection] = useState('asc')

  const [units] = usePolling(
    () => getClient().listUnits(sortKey, sortDirection),
    [],
    [refreshKey, sortKey, sortDirection],
  )

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function handleAction() {
    if (!actionConfirm) return
    setError(null)
    setSuccess(null)
    try {
      await getClient().setUnitStatus(actionConfirm.name, actionConfirm.action)
      setSuccess(
        `${actionConfirm.action} "${actionConfirm.name}" succeeded`,
      )
      setActionConfirm(null)
      doRefresh()
    } catch (err) {
      setError(err.message)
      setActionConfirm(null)
    }
  }

  const columns = [
    {
      key: 'Name',
      label: 'Service',
      transform: (v) => (
        <span className="font-mono text-sm">
          {v?.replace('.service', '') ?? v}
        </span>
      ),
    },
    { key: 'Description', label: 'Description' },
    {
      key: 'LoadState',
      label: 'Loaded',
      transform: (v) => (
        <Badge variant={v === 'loaded' ? 'outline' : 'destructive'}>
          {v}
        </Badge>
      ),
    },
    {
      key: 'ActiveState',
      label: 'Active',
      transform: (v) => {
        const variant =
          v === 'active'
            ? 'default'
            : v === 'failed'
              ? 'destructive'
              : 'secondary'
        return <Badge variant={variant}>{v}</Badge>
      },
    },
    {
      key: 'SubState',
      label: 'State',
      transform: (v) => <span className="text-sm">{v}</span>,
    },
    {
      key: '_actions',
      label: 'Actions',
      sortable: false,
      transform: (_, row) => (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm">
              <MoreHorizontal className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'start' })
              }
            >
              <Play className="h-3 w-3 mr-2" />
              Start
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'stop' })
              }
            >
              <Square className="h-3 w-3 mr-2" />
              Stop
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'restart' })
              }
            >
              <RotateCcw className="h-3 w-3 mr-2" />
              Restart
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'enable' })
              }
            >
              <Check className="h-3 w-3 mr-2" />
              Enable
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'disable' })
              }
            >
              <X className="h-3 w-3 mr-2" />
              Disable
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Services</h1>
        <p className="text-muted-foreground">Manage systemd units</p>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {success && (
        <Alert>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      )}

      <DataTable
        data={units}
        columns={columns}
        entryKey="Name"
        page={page}
        setPage={setPage}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={(key, dir) => {
          setSortKey(key)
          setSortDirection(dir)
        }}
        onReset={() => {
          setSortKey('Name')
          setSortDirection('asc')
        }}
      />

      <ConfirmDialog
        open={!!actionConfirm}
        title={`${actionConfirm?.action} service`}
        onConfirm={handleAction}
        onCancel={() => setActionConfirm(null)}
        confirmLabel={actionConfirm?.action}
      >
        {actionConfirm?.action}{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {actionConfirm?.name}
        </code>
        ?
      </ConfirmDialog>
    </div>
  )
}
