import { useState } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import { Badge } from '@/components/ui/badge'

const PAGE_SIZE = 20

function formatTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  return d.toLocaleString()
}

export default function AuditLog() {
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('id')
  const [sortDirection, setSortDirection] = useState('desc')

  const [auditData] = usePolling(
    () =>
      getClient().listAuditLog({
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
        sort_by: sortKey,
        sort_order: sortDirection,
      }),
    { entries: [], has_more: false },
    [page, sortKey, sortDirection],
    10000,
  )

  const entries = auditData.entries || []

  function handleSortChange(key, direction) {
    setSortKey(key)
    setSortDirection(direction)
  }

  function handleReset() {
    setSortKey('id')
    setSortDirection('desc')
  }

  const columns = [
    {
      key: 'id',
      label: 'ID',
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'created_at',
      label: 'Time',
      transform: (v) => (
        <span className="text-sm">{formatTime(v)}</span>
      ),
    },
    { key: 'action', label: 'Action' },
    {
      key: 'path',
      label: 'Endpoint',
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    { key: 'account', label: 'User', transform: (v) => v || '-' },
    {
      key: 'success',
      label: 'Status',
      transform: (v, row) =>
        v ? (
          <Badge variant="outline">Success</Badge>
        ) : (
          <Badge variant="destructive" title={row.error}>
            Error
          </Badge>
        ),
    },
    {
      key: 'error',
      label: 'Detail',
      transform: (v) =>
        v ? (
          <span className="text-sm text-destructive truncate max-w-[200px] block">
            {v}
          </span>
        ) : (
          '-'
        ),
    },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Audit Log</h1>
        <p className="text-muted-foreground">System activity history</p>
      </div>

      <DataTable
        data={entries}
        columns={columns}
        entryKey="id"
        page={page}
        setPage={setPage}
        pageSize={PAGE_SIZE}
        hasMore={auditData.has_more}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={handleSortChange}
        onReset={handleReset}
      />
    </div>
  )
}
