import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import getClient from '@/lib/client-instance.js'
import { useRequireAuth, usePolling } from '@/lib/hooks.js'
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
  RefreshCw,
  X,
  FileText,
  Terminal,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  Loader2,
} from 'lucide-react'
import { useI18n } from '@/i18n/I18nContext.jsx'

export default function SystemManagement() {
  const { t } = useI18n()
  const account = useRequireAuth()
  useEffect(() => { document.title = t('system.page_title') }, [t])
  const [searchParams] = useSearchParams()
  const [actionConfirm, setActionConfirm] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [refreshDialog, setRefreshDialog] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const pollRef = useRef(null)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('package_identifier')
  const [sortDirection, setSortDirection] = useState('asc')
  const [journalUnit, setJournalUnit] = useState(null)
  const [journalPriority, setJournalPriority] = useState(0)

  const [actionInProgress, setActionInProgress] = useState(false)
  const actionToastId = useRef('svc-action-progress')

  const [searchTerm, setSearchTerm] = useState('')
  const [customLogDialog, setCustomLogDialog] = useState(false)
  const [customLogUnit, setCustomLogUnit] = useState('')

  const [systemServicesOpen, setSystemServicesOpen] = useState(
    searchParams.get('expand') === 'system'
  )

  const [systemServices] = usePolling(
    () => getClient().listSystemServices().catch(() => []),
    [],
    [refreshKey],
    15000,
  )

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  const handleRefreshServices = useCallback(async () => {
    try {
      setRefreshing(true)
      await getClient().refreshSystemServices()
      toast.success(t('system.refresh_toast_started'))
      // After 3s delay, start polling ping to detect when systemcontroller returns.
      setTimeout(() => {
        pollRef.current = setInterval(async () => {
          try {
            await getClient().ping()
            clearInterval(pollRef.current)
            pollRef.current = null
            setRefreshing(false)
            setRefreshDialog(false)
            toast.success(t('system.refresh_toast_complete'))
            window.location.reload()
          } catch {
            // Controller not back yet.
          }
        }, 2000)
      }, 3000)
    } catch (err) {
      setRefreshing(false)
      toast.error(err.detail || err.message)
    }
  }, [t])

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

  const toastKeyByAction = useMemo(() => ({
    start: 'system.toast_starting',
    stop: 'system.toast_stopping',
    restart: 'system.toast_restarting',
  }), [])

  function pollUnitState(type, identifier, targetState, timeout = 30000) {
    return new Promise((resolve) => {
      const startTime = Date.now()
      const intervalId = setInterval(async () => {
        try {
          let currentState
          if (type === 'package') {
            const result = await getClient().listUnits(
              undefined, undefined, undefined, undefined, identifier,
            )
            const unit = result.entries.find((u) => u.Name === identifier)
            currentState = unit?.ActiveState
          } else {
            const services = await getClient().listSystemServices()
            const svc = services.find((s) => s.key === identifier)
            currentState = svc?.ActiveState
          }
          if (currentState === targetState) {
            clearInterval(intervalId)
            resolve(true)
          } else if (Date.now() - startTime > timeout) {
            clearInterval(intervalId)
            resolve(false)
          }
        } catch {
          if (Date.now() - startTime > timeout) {
            clearInterval(intervalId)
            resolve(false)
          }
        }
      }, 2000)
    })
  }

  async function handleAction() {
    if (!actionConfirm) return
    const { name, action } = actionConfirm
    const targetState = action === 'stop' ? 'inactive' : 'active'
    const capitalAction = `${action[0].toUpperCase()}${action.slice(1)}`

    setActionInProgress(true)
    toast.loading(t(toastKeyByAction[action], { name }), { id: actionToastId.current })

    try {
      await getClient().setUnitStatus(name, action)
      toast.loading(t('system.toast_action_waiting', { state: targetState }), { id: actionToastId.current })

      const reached = await pollUnitState('package', name, targetState)
      toast.dismiss(actionToastId.current)

      if (reached) {
        toast.success(t('system.toast_action_success', { action: capitalAction, name }))
      } else {
        toast.warning(t('system.toast_action_timeout', { action: capitalAction, name }))
      }
    } catch (err) {
      toast.dismiss(actionToastId.current)
      toast.error(err.detail || err.message)
    } finally {
      setActionInProgress(false)
      setActionConfirm(null)
      doRefresh()
    }
  }

  async function handleSystemServiceAction(key, action) {
    const targetState = action === 'stop' ? 'inactive' : 'active'
    const capitalAction = `${action[0].toUpperCase()}${action.slice(1)}`
    const displayName = systemServices.find((s) => s.key === key)?.display_name || key

    setActionInProgress(true)
    toast.loading(t(toastKeyByAction[action], { name: displayName }), { id: actionToastId.current })

    try {
      await getClient().setSystemServiceStatus(key, action)
      toast.loading(t('system.toast_action_waiting', { state: targetState }), { id: actionToastId.current })

      const reached = await pollUnitState('system', key, targetState)
      toast.dismiss(actionToastId.current)

      if (reached) {
        toast.success(t('system.toast_action_success', { action: capitalAction, name: displayName }))
      } else {
        toast.warning(t('system.toast_action_timeout', { action: capitalAction, name: displayName }))
      }
    } catch (err) {
      toast.dismiss(actionToastId.current)
      toast.error(err.detail || err.message)
    } finally {
      setActionInProgress(false)
      doRefresh()
    }
  }

  const columns = [
    {
      key: 'package_identifier',
      label: t('system.col_package'),
      transform: (v) => (
        <span className="font-mono text-sm">
          {v || '—'}
        </span>
      ),
    },
    { key: 'package_description', label: t('system.col_description') },
    {
      key: 'ActiveState',
      label: t('system.col_status'),
      sortValues: ['active', 'inactive', 'failed'],
      transform: (v, row) => {
        const variant =
          v === 'active'
            ? 'default'
            : v === 'failed'
              ? 'destructive'
              : 'secondary'
        const label = row.nc_failed ? t('system.status_failed_nc') : v
        return <Badge variant={variant}>{label}</Badge>
      },
    },
    {
      key: '_actions',
      label: t('system.col_actions'),
      sortable: false,
      transform: (_, row) => (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" aria-label={t('system.actions_label')}>
              <MoreHorizontal className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem
              disabled={actionInProgress}
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'start' })
              }
            >
              <Play className="h-3 w-3 mr-2" />
              {t('system.action_start')}
            </DropdownMenuItem>
            {row.Name !== 'town-os-systemcontroller.service' && (
              <DropdownMenuItem
                disabled={actionInProgress}
                onClick={() =>
                  setActionConfirm({ name: row.Name, action: 'stop' })
                }
              >
                <Square className="h-3 w-3 mr-2" />
                {t('system.action_stop')}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem
              disabled={actionInProgress}
              onClick={() =>
                setActionConfirm({ name: row.Name, action: 'restart' })
              }
            >
              <RotateCcw className="h-3 w-3 mr-2" />
              {t('system.action_restart')}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => openJournal(row.Name)}>
              <FileText className="h-3 w-3 mr-2" />
              {t('system.action_service_logs')}
            </DropdownMenuItem>
            {row.nc_active && (
              <DropdownMenuItem onClick={() => {
                const ncUnit = row.Name.replace('.service', '-network.service')
                openJournal(ncUnit)
              }}>
                <FileText className="h-3 w-3 mr-2" />
                {t('system.action_network_logs')}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('system.title')}</h1>
          <p className="text-muted-foreground">{t('system.description')}</p>
        </div>
        {account?.admin && (
          <Button variant="outline" onClick={() => setRefreshDialog(true)}>
            <RefreshCw className="h-4 w-4 mr-1" />
            {t('system.refresh_btn')}
          </Button>
        )}
      </div>

      {/* System Services Section */}
      {systemServices.length > 0 && (
        <div>
          <Button
            variant="ghost"
            className="w-full justify-start px-4 py-3 h-auto border rounded-lg"
            onClick={() => setSystemServicesOpen((v) => !v)}
          >
            <div className="flex items-center gap-2">
              {systemServicesOpen ? (
                <ChevronDown className="h-4 w-4" />
              ) : (
                <ChevronRight className="h-4 w-4" />
              )}
              <span className="font-semibold">{t('system.system_services_title')}</span>
              {systemServices.filter((s) => s.ActiveState === 'failed').length > 0 && (
                <Badge variant="destructive">
                  {t('nav.system_services_down', {
                    count: systemServices.filter((s) => s.ActiveState === 'failed').length,
                    s: systemServices.filter((s) => s.ActiveState === 'failed').length === 1 ? '' : 's',
                  })}
                </Badge>
              )}
              <span className="text-muted-foreground text-sm">{t('system.system_services_description')}</span>
            </div>
          </Button>
          {systemServicesOpen && (
            <div className="border rounded-lg mt-2 overflow-hidden">
              <table className="w-full">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="text-left px-4 py-2 text-sm font-medium">Service</th>
                    <th className="text-left px-4 py-2 text-sm font-medium">{t('system.col_status')}</th>
                    <th className="px-4 py-2 text-sm font-medium">
                      <div className="text-right pr-2">{t('system.col_actions')}</div>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {systemServices.map((svc) => {
                    const isActive = svc.ActiveState === 'active'
                    const isFailed = svc.ActiveState === 'failed'
                    const variant = isActive ? 'default' : isFailed ? 'destructive' : 'secondary'
                    // The systemcontroller cannot be stopped from its own
                    // HTTP handler — that would kill the process serving
                    // this request. Hide the Stop action for the self
                    // entry; Restart is still available (systemd respawns).
                    const isSelf = svc.key === 'systemcontroller'
                    return (
                      <tr key={svc.key} className="border-b last:border-b-0">
                        <td className="px-4 py-2 text-sm font-medium">{svc.display_name}</td>
                        <td className="px-4 py-2">
                          <Badge variant={variant}>
                            {svc.ActiveState || 'unknown'}
                          </Badge>
                        </td>
                        <td className="px-4 py-2 text-right">
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button variant="ghost" size="sm" aria-label={t('system.actions_label')}>
                                <MoreHorizontal className="h-4 w-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent>
                              <DropdownMenuItem disabled={actionInProgress} onClick={() => handleSystemServiceAction(svc.key, 'start')}>
                                <Play className="h-3 w-3 mr-2" />
                                {t('system.action_start')}
                              </DropdownMenuItem>
                              {!isSelf && (
                                <DropdownMenuItem disabled={actionInProgress} onClick={() => handleSystemServiceAction(svc.key, 'stop')}>
                                  <Square className="h-3 w-3 mr-2" />
                                  {t('system.action_stop')}
                                </DropdownMenuItem>
                              )}
                              <DropdownMenuItem disabled={actionInProgress} onClick={() => handleSystemServiceAction(svc.key, 'restart')}>
                                <RotateCcw className="h-3 w-3 mr-2" />
                                {t('system.action_restart')}
                              </DropdownMenuItem>
                              <DropdownMenuItem onClick={() => openJournal(svc.Name || svc.unit_name)}>
                                <FileText className="h-3 w-3 mr-2" />
                                {t('system.action_service_logs')}
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {unitsLoading && units.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">{t('system.loading')}</div>
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
          {t('system.advanced_logs_btn')}
        </Button>
      </div>

      {/* Advanced Logs Dialog */}
      <Dialog open={customLogDialog} onOpenChange={(v) => !v && setCustomLogDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('system.advanced_logs_title')}</DialogTitle>
            <DialogDescription>{t('system.advanced_logs_description')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="flex flex-col gap-2">
              <Button variant="outline" className="justify-start" onClick={() => {
                setCustomLogDialog(false)
                openJournal('__system__')
              }}>
                <Terminal className="h-4 w-4 mr-2" />
                {t('system.system_logs')}
              </Button>
              <Button variant="outline" className="justify-start" onClick={() => {
                setCustomLogDialog(false)
                openJournal('__system__', 3)
              }}>
                <X className="h-4 w-4 mr-2" />
                {t('system.journal_errors')}
              </Button>
            </div>
            <div className="space-y-2">
              <Label htmlFor="custom-log-unit">{t('system.custom_service_label')}</Label>
              <div className="flex gap-2">
                <Input
                  id="custom-log-unit"
                  placeholder={t('system.custom_service_placeholder')}
                  value={customLogUnit}
                  onChange={(e) => setCustomLogUnit(e.target.value)}
                />
                <Button disabled={!customLogUnit.trim()} onClick={() => {
                  const unit = customLogUnit.trim()
                  setCustomLogDialog(false)
                  openJournal(unit)
                }}>
                  {t('system.view_btn')}
                </Button>
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!actionConfirm}
        title={t('system.confirm_action_title', { action: `${actionConfirm?.action?.[0]?.toUpperCase()}${actionConfirm?.action?.slice(1)}` })}
        onConfirm={handleAction}
        onCancel={() => setActionConfirm(null)}
        confirmLabel={`${actionConfirm?.action?.[0]?.toUpperCase()}${actionConfirm?.action?.slice(1)}`}
        loading={actionInProgress}
      >
        {t('system.confirm_action_message', { action: `${actionConfirm?.action?.[0]?.toUpperCase()}${actionConfirm?.action?.slice(1)}`, name: actionConfirm?.name })}
      </ConfirmDialog>

      {/* Refresh Core Services Dialog */}
      <Dialog open={refreshDialog} onOpenChange={(v) => !refreshing && !v && setRefreshDialog(false)}>
        <DialogContent onPointerDownOutside={refreshing ? (e) => e.preventDefault() : undefined}>
          <DialogHeader>
            <DialogTitle>{t('system.refresh_dialog_title')}</DialogTitle>
            <DialogDescription className="sr-only">{t('system.refresh_dialog_title')}</DialogDescription>
          </DialogHeader>
          {refreshing ? (
            <div className="flex flex-col items-center gap-4 py-6">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
              <p className="text-sm text-muted-foreground">{t('system.refresh_in_progress')}</p>
            </div>
          ) : (
            <>
              <div className="rounded-lg border border-destructive bg-destructive/10 p-4 space-y-2">
                <div className="flex items-start gap-2">
                  <AlertTriangle className="h-5 w-5 text-destructive mt-0.5 shrink-0" />
                  <div className="space-y-1 text-sm">
                    <p>{t('system.refresh_warning_1')}</p>
                    <p>{t('system.refresh_warning_2')}</p>
                    <p className="font-semibold">{t('system.refresh_warning_3')}</p>
                  </div>
                </div>
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" onClick={() => setRefreshDialog(false)}>
                  {t('confirm.default_cancel_label')}
                </Button>
                <Button variant="destructive" onClick={handleRefreshServices}>
                  {t('system.refresh_confirm_btn')}
                </Button>
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>

      <JournalViewer
        journalUnit={journalUnit}
        onClose={() => setJournalUnit(null)}
        units={units}
        initialPriority={journalPriority}
      />
    </div>
  )
}
