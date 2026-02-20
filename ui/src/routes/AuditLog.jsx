import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { JsonTree } from '@/lib/json-tree.jsx'
import DataTable from '@/components/DataTable.jsx'
import { Button } from '@/components/ui/button'
import { Check, CircleAlert, FileText } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

const PAGE_SIZE = 20

function formatTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  return d.toLocaleString()
}

export default function AuditLog() {
  useEffect(() => { document.title = 'Town OS - Audit Log' }, [])
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('id')
  const [sortDirection, setSortDirection] = useState('desc')
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailData, setDetailData] = useState('')
  const [detailAction, setDetailAction] = useState('')
  const [errorOpen, setErrorOpen] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  const [auditData, , auditLoading] = usePolling(
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

  function openDetail(row) {
    setDetailData(row.detail || '')
    setDetailAction(row.action || row.path || '')
    setDetailOpen(true)
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
      key: 'detail',
      label: 'Detail',
      sortable: false,
      transform: (v, row) =>
        v ? (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 w-6 p-0"
            onClick={() => openDetail(row)}
            title="View request parameters"
          >
            <FileText className="h-3.5 w-3.5" />
          </Button>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'success',
      label: 'Status',
      transform: (v, row) =>
        v ? (
          <Check className="h-4 w-4 text-green-600" />
        ) : (
          <CircleAlert
            className="h-4 w-4 text-destructive cursor-pointer"
            onClick={() => {
              setErrorMessage(row.error || 'Unknown error')
              setErrorOpen(true)
            }}
          />
        ),
    },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Audit Log</h1>
        <p className="text-muted-foreground">System activity history</p>
      </div>

      {auditLoading && entries.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      <DataTable
        data={entries}
        columns={columns}
        entryKey="id"
        page={page}
        setPage={setPage}
        pageSize={PAGE_SIZE}
        hasMore={auditData.has_more}
        totalPages={auditData.total_pages}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={handleSortChange}
        onReset={handleReset}
      />

      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="sm:max-w-md max-h-[70vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Request Parameters: {detailAction}</DialogTitle>
          </DialogHeader>
          <div className="overflow-auto flex-1 min-h-0 rounded border bg-muted p-3">
            <JsonTree data={detailData} />
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={errorOpen} onOpenChange={setErrorOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <CircleAlert className="h-4 w-4 text-destructive" />
              Error
            </DialogTitle>
          </DialogHeader>
          <div className="rounded border bg-muted p-3 break-words whitespace-pre-wrap font-mono text-sm">
            {errorMessage}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
