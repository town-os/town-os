import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { PAGE_SIZE } from '@/lib/utils.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import JournalViewer from '@/components/JournalViewer.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  MoreHorizontal,
  Play,
  Square,
  RotateCcw,
  X,
  FileText,
  Terminal,
} from 'lucide-react'

export default function SystemManagement() {
  useEffect(() => { document.title = 'Town OS - Services' }, [])
  const [actionConfirm, setActionConfirm] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('package_identifier')
  const [sortDirection, setSortDirection] = useState('asc')
  const [journalUnit, setJournalUnit] = useState(null)
  const [journalPriority, setJournalPriority] = useState(0)

  const [searchTerm, setSearchTerm] = useState('')
  const [customLogDialog, setCustomLogDialog] = useState(false)
  const [customLogUnit, setCustomLogUnit] = useState('')

  const effectiveSearch = searchTerm

  const [unitData, , unitsLoading] = usePolling(
    () => getClient().listUnits(sortKey, sortDirection, PAGE_SIZE, page * PAGE_SIZE, effectiveSearch),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, sortKey, sortDirection, page, effectiveSearch],
  )
  const units = unitData.entries || []

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  function openJournal(unitName, priority = 0) {
    setJournalUnit(null)
    setJournalPriority(priority)
    // Use a microtask so the JournalViewer unmounts and remounts with fresh state.
    queueMicrotask(() => {
      setJournalUnit(unitName)
    })
  }

  async function handleAction() {
    if (!actionConfirm) return
    try {
      await getClient().setUnitStatus(actionConfirm.name, actionConfirm.action)
      toast.success(
        `${actionConfirm.action} "${actionConfirm.name}" succeeded`,
      )
      setActionConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setActionConfirm(null)
    }
  }

  const columns = [
    {
      key: 'package_identifier',
      label: 'Package',
      transform: (v) => (
        <span className="font-mono text-sm">
          {v || '—'}
        </span>
      ),
    },
    { key: 'package_description', label: 'Description' },
    {
      key: 'ActiveState',
      label: 'Status',
      sortValues: ['active', 'inactive', 'failed'],
      transform: (v, row) => {
        const variant =
          v === 'active'
            ? 'default'
            : v === 'failed'
              ? 'destructive'
              : 'secondary'
        const label = row.nc_failed ? 'failed (NC)' : v
        return <Badge variant={variant}>{label}</Badge>
      },
    },
    {
      key: '_actions',
      label: 'Actions',
      sortable: false,
      transform: (_, row) => (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" aria-label="Service actions">
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
            {row.Name !== 'town-os-systemcontroller.service' && (
              <DropdownMenuItem
                onClick={() =>
                  setActionConfirm({ name: row.Name, action: 'stop' })
                }
              >
                <Square className="h-3 w-3 mr-2" />
                Stop
              </DropdownMenuItem>
            )}
            <DropdownMenuItem
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'restart' })
              }
            >
              <RotateCcw className="h-3 w-3 mr-2" />
              Restart
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => openJournal(row.Name)}>
              <FileText className="h-3 w-3 mr-2" />
              Service Logs
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => {
              const ncUnit = row.Name.replace('.service', '-network.service')
              openJournal(ncUnit)
            }}>
              <FileText className="h-3 w-3 mr-2" />
              Network Logs
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Services</h1>
          <p className="text-muted-foreground">Manage installed packages</p>
        </div>
      </div>

      {unitsLoading && units.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      <DataTable
        data={units}
        columns={columns}
        entryKey="Name"
        page={page}
        setPage={setPage}
        hasMore={unitData.has_more}
        totalPages={unitData.total_pages}
        totalCount={unitData.total_count}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={(key, dir) => {
          setSortKey(key)
          setSortDirection(dir)
          setPage(0)
        }}
        onReset={() => {
          setSortKey('package_identifier')
          setSortDirection('asc')
          setSearchTerm('')
          setPage(0)
        }}
        onSearchChange={(s) => {
          setSearchTerm(s)
          setPage(0)
        }}
      />

      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={() => setCustomLogDialog(true)}>
          <Terminal className="h-4 w-4 mr-1" />
          Advanced Logs
        </Button>
      </div>

      {/* Advanced Logs Dialog */}
      <Dialog open={customLogDialog} onOpenChange={(v) => !v && setCustomLogDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Advanced Logs</DialogTitle>
            <DialogDescription>Quick access to common log views or enter a custom service name.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="flex flex-col gap-2">
              <Button variant="outline" className="justify-start" onClick={() => {
                setCustomLogDialog(false)
                openJournal('town-os-systemcontroller.service')
              }}>
                <FileText className="h-4 w-4 mr-2" />
                Controller Logs
              </Button>
              <Button variant="outline" className="justify-start" onClick={() => {
                setCustomLogDialog(false)
                openJournal('__system__')
              }}>
                <Terminal className="h-4 w-4 mr-2" />
                System Logs
              </Button>
              <Button variant="outline" className="justify-start" onClick={() => {
                setCustomLogDialog(false)
                openJournal('__system__', 3)
              }}>
                <X className="h-4 w-4 mr-2" />
                Journal Errors
              </Button>
            </div>
            <div className="space-y-2">
              <Label htmlFor="custom-log-unit">Custom service name</Label>
              <div className="flex gap-2">
                <Input
                  id="custom-log-unit"
                  placeholder="e.g. sshd.service"
                  value={customLogUnit}
                  onChange={(e) => setCustomLogUnit(e.target.value)}
                />
                <Button disabled={!customLogUnit.trim()} onClick={() => {
                  const unit = customLogUnit.trim()
                  setCustomLogDialog(false)
                  openJournal(unit)
                }}>
                  View
                </Button>
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!actionConfirm}
        title={`${actionConfirm?.action?.[0]?.toUpperCase()}${actionConfirm?.action?.slice(1)} service`}
        onConfirm={handleAction}
        onCancel={() => setActionConfirm(null)}
        confirmLabel={`${actionConfirm?.action?.[0]?.toUpperCase()}${actionConfirm?.action?.slice(1)}`}
      >
        {`${actionConfirm?.action?.[0]?.toUpperCase()}${actionConfirm?.action?.slice(1)}`}{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {actionConfirm?.name}
        </code>
        ?
      </ConfirmDialog>

      <JournalViewer
        journalUnit={journalUnit}
        onClose={() => setJournalUnit(null)}
        units={units}
        initialPriority={journalPriority}
      />
    </div>
  )
}
