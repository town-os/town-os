import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { PAGE_SIZE } from '@/lib/utils.js'
import { JsonTree } from '@/lib/json-tree.jsx'
import DataTable from '@/components/DataTable.jsx'
import JournalViewer from '@/components/JournalViewer.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Check, CircleAlert, FileText } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useI18n } from '@/i18n/I18nContext.jsx'

function formatTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  return d.toLocaleString()
}

function parseErrorDetail(raw, fallback) {
  if (!raw) return { message: fallback }
  // Echo error format: "code=401, message=invalid credentials"
  const echoMatch = raw.match(/^code=(\d+),\s*message=(.+)$/)
  if (echoMatch) {
    return { status: parseInt(echoMatch[1], 10), message: echoMatch[2] }
  }
  return { message: raw }
}

function ErrorDetail({ row, t }) {
  const parsed = parseErrorDetail(row.error, t('audit.error_unknown'))
  return (
    <div className="rounded-lg border border-destructive/30 bg-destructive/5 overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-3 border-b border-destructive/20 bg-destructive/10">
        <CircleAlert className="h-4 w-4 text-destructive shrink-0" />
        <span className="font-medium text-destructive text-sm">
          {t('audit.error_failed', { action: row.action || row.path || t('audit.error_request_label') })}
        </span>
        {parsed.status && (
          <Badge variant="destructive" className="ml-auto font-mono text-xs">
            {parsed.status}
          </Badge>
        )}
      </div>
      <div className="px-4 py-3 space-y-2">
        <p className="text-sm">{parsed.message}</p>
        {(row.path || row.account) && (
          <div className="flex gap-4 text-xs text-muted-foreground pt-1">
            {row.path && <span className="font-mono">{row.path}</span>}
            {row.account && <span>{row.account}</span>}
          </div>
        )}
      </div>
    </div>
  )
}

export default function AuditLog() {
  const { t } = useI18n()
  useEffect(() => { document.title = t('audit.page_title') }, [t])
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('id')
  const [sortDirection, setSortDirection] = useState('desc')
  const [search, setSearch] = useState('')
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailData, setDetailData] = useState('')
  const [detailAction, setDetailAction] = useState('')
  const [errorOpen, setErrorOpen] = useState(false)
  const [errorRow, setErrorRow] = useState(null)
  const [journalUnit, setJournalUnit] = useState(null)
  const [journalTimestamp, setJournalTimestamp] = useState(null)

  const [auditData, , auditLoading] = usePolling(
    () =>
      getClient().listAuditLog({
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
        sort_by: sortKey,
        sort_order: sortDirection,
        search: search || undefined,
      }),
    { entries: [], has_more: false },
    [page, sortKey, sortDirection, search],
    10000,
  )

  const entries = auditData.entries || []

  function handleSortChange(key, direction) {
    setSortKey(key)
    setSortDirection(direction)
    setPage(0)
  }

  function handleReset() {
    setSortKey('id')
    setSortDirection('desc')
    setSearch('')
    setPage(0)
  }

  function openDetail(row) {
    setDetailData(row.detail || '')
    setDetailAction(row.action || row.path || '')
    setDetailOpen(true)
  }

  function openJournal(timestamp) {
    setJournalUnit(null)
    setJournalTimestamp(timestamp)
    queueMicrotask(() => {
      setJournalUnit('__system__')
    })
  }

  const columns = [
    // Widths are explicit because the table is table-layout:fixed: left to
    // itself it splits the pane equally, which starves the timestamp and the
    // endpoint -- the two columns with the longest content -- while handing a
    // full share to an icon. Content that does not fit a fixed cell can neither
    // wrap (cells are nowrap) nor shrink, so it used to overlap the next column.
    //
    // The endpoint and the account hold the two longest strings by far (a
    // request path, an email address), so they take the width the icon columns
    // do not need.
    {
      key: 'id',
      label: t('audit.col_id'),
      width: '6%',
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'created_at',
      label: t('audit.col_time'),
      width: '17%',
      // The timestamp itself opens the journal at that moment. This used to
      // be a trailing Clock icon button, but it sat flush against the Action
      // column and read as a stray leading icon on the action text.
      transform: (v) => (
        <button
          type="button"
          className="text-sm hover:underline"
          onClick={() => openJournal(v)}
          aria-label={t('audit.view_logs_label')}
          title={t('audit.view_logs_label')}
        >
          {formatTime(v)}
        </button>
      ),
    },
    {
      key: 'action',
      label: t('audit.col_action'),
      width: '14%',
    },
    {
      key: 'path',
      label: t('audit.col_endpoint'),
      width: '32%',
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'account',
      label: t('audit.col_user'),
      // An email address is long, and at 15% it was ellipsed down to nothing
      // useful while pressing against the endpoint beside it.
      width: '21%',
      transform: (v) => v || '-',
    },
    {
      key: 'detail',
      // Holds one 24px icon button and nothing else, so it gets the width of the
      // icon plus its own cell padding -- and the padding is cut to the minimum,
      // because every pixel here is one the endpoint and the account do not get.
      label: t('audit.col_detail'),
      width: '5%',
      className: 'px-1 text-center',
      sortable: false,
      transform: (v, row) =>
        v ? (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 w-6 p-0"
            onClick={() => openDetail(row)}
            aria-label={t('audit.view_params_label')}
            title={t('audit.view_params_label')}
          >
            <FileText className="h-3.5 w-3.5" />
          </Button>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'success',
      label: t('audit.col_status'),
      width: '5%',
      sortable: false,
      className: 'text-center',
      transform: (v, row) =>
        v ? (
          <div className="flex items-center justify-center">
            <span role="status" aria-label={t('audit.success_label')}>
              <Check className="h-4 w-4 text-green-600" aria-hidden="true" />
              <span className="sr-only">{t('audit.success_label')}</span>
            </span>
          </div>
        ) : (
          <div className="flex items-center justify-center">
            <button
              className="inline-flex items-center"
              onClick={() => {
                setErrorRow(row)
                setErrorOpen(true)
              }}
              aria-label={t('audit.view_error_label')}
            >
              <CircleAlert className="h-4 w-4 text-destructive cursor-pointer" aria-hidden="true" />
              <span className="sr-only">{t('audit.error_label')}</span>
            </button>
          </div>
        ),
    },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('audit.title')}</h1>
        <p className="text-muted-foreground">{t('audit.description')}</p>
      </div>

      {auditLoading && entries.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">{t('audit.loading')}</div>
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
        totalCount={auditData.total_count}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={handleSortChange}
        onReset={handleReset}
        onSearchChange={(s) => {
          setSearch(s)
          setPage(0)
        }}
      />

      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="sm:max-w-md max-h-[70vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>{t('audit.detail_dialog_title')}: {detailAction}</DialogTitle>
            <DialogDescription>{t('audit.detail_dialog_description')}</DialogDescription>
          </DialogHeader>
          <div className="overflow-auto flex-1 min-h-0 rounded border bg-muted p-3">
            <JsonTree data={detailData} />
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={errorOpen} onOpenChange={setErrorOpen}>
        <DialogContent className="sm:max-w-md max-h-[70vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <CircleAlert className="h-4 w-4 text-destructive" />
              {t('audit.error_dialog_title')}
            </DialogTitle>
            <DialogDescription>{t('audit.error_dialog_description')}</DialogDescription>
          </DialogHeader>
          {errorRow && <ErrorDetail row={errorRow} t={t} />}
        </DialogContent>
      </Dialog>

      <JournalViewer
        journalUnit={journalUnit}
        onClose={() => setJournalUnit(null)}
        units={[]}
        initialTimestamp={journalTimestamp}
      />
    </div>
  )
}
