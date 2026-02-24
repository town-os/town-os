import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { parseAnsi, parseFields, stripAnsi, groupByMinute } from '@/lib/log-format.js'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { useJournalSearch } from '@/lib/use-journal-search.js'
import { useFollowMode } from '@/lib/use-follow-mode.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
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
import { DialogFooter } from '@/components/ui/dialog'
import {
  MoreHorizontal,
  Play,
  Square,
  RotateCcw,
  X,
  FileText,
  Copy,
  ClipboardCheck,
  Triangle,
  Search,
  Clock,
  Terminal,
} from 'lucide-react'

/** Render parsed field segments as styled JSX spans. */
function renderFields(text, keyPrefix) {
  const fields = parseFields(text)
  if (fields.length === 0) return <span style={{ color: '#444' }}>{text}</span>
  if (fields.length === 1 && fields[0].type === 'text') return <span style={{ color: '#444' }}>{text}</span>
  return fields.map((f, i) => {
    if (f.type === 'text') {
      return <span key={`${keyPrefix}-t${i}`} style={{ color: '#444' }}>{f.value}</span>
    }
    return [
      <span key={`${keyPrefix}-k${i}`} style={{ color: '#666', fontWeight: 'bold' }}>{f.name}</span>,
      <span key={`${keyPrefix}-e${i}`} style={{ color: '#555' }}>{f.eq}</span>,
      <span key={`${keyPrefix}-v${i}`} style={{ color: '#555' }}>{f.value}</span>,
    ]
  })
}

function formatMessage(text) {
  const segments = parseAnsi(text)

  if (segments.length === 0) {
    return renderFields(text, 'f')
  }

  return segments.map((seg, i) => {
    if (seg.color || seg.bold) {
      const style = {}
      if (seg.color) style.color = seg.color
      if (seg.bold) style.fontWeight = 'bold'
      return <span key={i} style={style}>{seg.str}</span>
    }
    return <span key={i}>{renderFields(seg.str, `s${i}`)}</span>
  })
}

export default function SystemManagement() {
  useEffect(() => { document.title = 'Town OS - Services' }, [])
  const [actionConfirm, setActionConfirm] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('Name')
  const [sortDirection, setSortDirection] = useState('asc')
  const [journalUnit, setJournalUnit] = useState(null)
  const [journalEntries, setJournalEntries] = useState([])
  const [journalCursor, setJournalCursor] = useState(null)
  const [journalLoading, setJournalLoading] = useState(false)
  const [journalHasMore, setJournalHasMore] = useState(false)
  const [journalInitial, setJournalInitial] = useState(true)
  const [copied, setCopied] = useState(false)
  const [expandedGroups, setExpandedGroups] = useState({})
  const [flatMode, setFlatMode] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [journalEndCursor, setJournalEndCursor] = useState(null)
  const [sinceDate, setSinceDate] = useState('')
  const [sinceHour, setSinceHour] = useState('')
  const [untilDate, setUntilDate] = useState('')
  const [untilHour, setUntilHour] = useState('')
  const [timeFilterOpen, setTimeFilterOpen] = useState(false)
  // Pending fields shown in the popout before the user commits.
  const [pendingSinceDate, setPendingSinceDate] = useState('')
  const [pendingSinceHour, setPendingSinceHour] = useState('')
  const [pendingUntilDate, setPendingUntilDate] = useState('')
  const [pendingUntilHour, setPendingUntilHour] = useState('')
  const hasTimeFilter = sinceDate !== '' || sinceHour !== '' || untilDate !== '' || untilHour !== ''
  const [followMode, setFollowMode, toggleFollow] = useFollowMode(searchQuery !== '' || hasTimeFilter)
  const today = useMemo(() => new Date().toISOString().slice(0, 10), [])
  const sinceTime = useMemo(() => {
    const d = sinceDate || (sinceHour !== '' ? today : '')
    if (!d) return ''
    if (sinceHour === '') return `${d}T00:00`
    return `${d}T${String(sinceHour).padStart(2, '0')}:00`
  }, [sinceDate, sinceHour, today])
  const untilTime = useMemo(() => {
    const d = untilDate || (untilHour !== '' ? today : '')
    if (!d) return ''
    if (untilHour === '') {
      const next = new Date(d)
      next.setDate(next.getDate() + 1)
      return `${next.toISOString().slice(0, 10)}T00:00`
    }
    return `${d}T${String(untilHour).padStart(2, '0')}:00`
  }, [untilDate, untilHour, today])
  const scrollRef = useRef(null)
  const followAppendRef = useRef(false)
  const wasAtBottomRef = useRef(true)

  const minuteGroups = useMemo(() => groupByMinute(journalEntries), [journalEntries])

  function toggleGroup(key) {
    setExpandedGroups((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  function toggleFlatMode() {
    setFlatMode((v) => !v)
  }


  const PAGE_SIZE = 20
  const [searchTerm, setSearchTerm] = useState('')
  const [customLogDialog, setCustomLogDialog] = useState(false)
  const [customLogUnit, setCustomLogUnit] = useState('')

  const effectiveSearch = searchTerm ? `town-os-package--${searchTerm}` : 'town-os-package--'

  const [unitData, , unitsLoading] = usePolling(
    () => getClient().listUnits(sortKey, sortDirection, PAGE_SIZE, page * PAGE_SIZE, effectiveSearch),
    { entries: [], has_more: false, total_pages: 1 },
    [refreshKey, sortKey, sortDirection, page, effectiveSearch],
  )
  const units = unitData.entries || []

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  function closeJournal() {
    setJournalUnit(null)
    setJournalEntries([])
    setJournalCursor(null)
    setJournalHasMore(false)
    setJournalInitial(true)
    setCopied(false)
    setExpandedGroups({})
    setFlatMode(false)
    setSearchQuery('')
    setFollowMode(true)
    setJournalEndCursor(null)
    setSinceDate('')
    setSinceHour('')
    setUntilDate('')
    setUntilHour('')
    setPendingSinceDate('')
    setPendingSinceHour('')
    setPendingUntilDate('')
    setPendingUntilHour('')
    setTimeFilterOpen(false)
    followAppendRef.current = false
    wasAtBottomRef.current = true
  }

  function copyJournal() {
    const text = journalEntries
      .map((e) => {
        const ts = new Date(e.RealtimeTimestamp).toLocaleString()
        const msg = stripAnsi(e.Message)
        return `${ts} ${msg}`
      })
      .join('\n')

    function onSuccess() {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }

    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(onSuccess)
      return
    }

    // Fallback for non-secure contexts (HTTP with non-localhost hostname).
    // Append inside the dialog so the focus trap doesn't block selection.
    const container = scrollRef.current || document.body
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    ta.style.opacity = '0'
    container.appendChild(ta)
    ta.focus()
    ta.select()
    try {
      document.execCommand('copy')
      onSuccess()
    } finally {
      container.removeChild(ta)
    }
  }

  const loadEntries = useCallback(async (unitName, beforeCursor, grep, since, until) => {
    setJournalLoading(true)
    const apiUnit = unitName === '__system__' ? '' : unitName
    try {
      const result = await getClient().logTail(apiUnit, 200, beforeCursor, undefined, grep || undefined, since || undefined, until || undefined)
      const entries = result.entries || []
      if (beforeCursor) {
        setJournalEntries((prev) => [...entries, ...prev])
      } else {
        setJournalEntries(entries)
        if (result.end_cursor) {
          setJournalEndCursor(result.end_cursor)
        }
      }
      setJournalCursor(result.cursor || null)
      setJournalHasMore(entries.length >= 200 && !!result.cursor)
    } catch {
      // ignore errors on load
    } finally {
      setJournalLoading(false)
    }
  }, [])

  function openJournal(unitName) {
    closeJournal()
    setJournalUnit(unitName)
    loadEntries(unitName, undefined, '')
  }

  function loadMore() {
    if (journalUnit && journalCursor) {
      const el = scrollRef.current
      const prevHeight = el?.scrollHeight ?? 0
      loadEntries(journalUnit, journalCursor, searchQuery).then(() => {
        requestAnimationFrame(() => {
          if (el) {
            el.scrollTop = el.scrollHeight - prevHeight
          }
        })
      })
    }
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

  // Re-fetch when search query or since time changes (debounced).
  useJournalSearch(journalUnit, searchQuery, sinceTime, untilTime, loadEntries)

  // Follow mode: poll for new entries.
  useEffect(() => {
    if (!journalUnit || !followMode || !journalEndCursor) return
    const apiUnit = journalUnit === '__system__' ? '' : journalUnit
    const timer = setInterval(async () => {
      try {
        const result = await getClient().logTail(apiUnit, 200, undefined, journalEndCursor, searchQuery || undefined)
        const entries = result.entries || []
        if (entries.length > 0) {
          const el = scrollRef.current
          if (el) {
            wasAtBottomRef.current = el.scrollTop + el.clientHeight >= el.scrollHeight - 20
          }
          followAppendRef.current = true
          setJournalEntries((prev) => [...prev, ...entries])
          setJournalEndCursor(result.end_cursor || null)
        }
      } catch {
        // ignore follow errors
      }
    }, 2000)
    return () => clearInterval(timer)
  }, [journalUnit, followMode, journalEndCursor, searchQuery])

  useEffect(() => {
    if (journalUnit && scrollRef.current && !journalLoading && journalInitial) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      setJournalInitial(false)
    } else if (followAppendRef.current && scrollRef.current) {
      if (wasAtBottomRef.current) {
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      }
      followAppendRef.current = false
    }
  }, [journalUnit, journalEntries.length, journalLoading, journalInitial])

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
      key: 'ActiveState',
      label: 'Status',
      sortValues: ['active', 'inactive', 'failed'],
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
              View Journal
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
          <p className="text-muted-foreground">Manage systemd units</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => openJournal('town-os-systemcontroller.service')}
        >
          <FileText className="h-4 w-4 mr-1" />
          Controller Logs
        </Button>
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
          setSortKey('Name')
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

      {/* Custom/System Log Dialog */}
      <Dialog open={customLogDialog} onOpenChange={(v) => !v && setCustomLogDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>View Service Logs</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <p className="text-sm text-muted-foreground">
              View logs for the overall system journal or any specific systemd service.
            </p>
            <div className="space-y-2">
              <Label htmlFor="custom-log-unit">Service name</Label>
              <Input
                id="custom-log-unit"
                placeholder="e.g. sshd.service or leave blank for system logs"
                value={customLogUnit}
                onChange={(e) => setCustomLogUnit(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Leave blank to view system-wide logs. Enter a full unit name for a specific service.
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCustomLogDialog(false)}>
              Cancel
            </Button>
            <Button onClick={() => {
              const unit = customLogUnit.trim() || '__system__'
              setCustomLogDialog(false)
              openJournal(unit)
            }}>
              View Logs
            </Button>
          </DialogFooter>
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

      <Dialog open={journalUnit !== null} onOpenChange={(open) => { if (!open) closeJournal() }}>
        <DialogContent className="sm:max-w-3xl max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <span>{journalUnit === '__system__' ? 'System Logs' : journalUnit?.replace('.service', '')}</span>
              {journalUnit && (() => {
                const unit = units.find((u) => u.Name === journalUnit)
                if (!unit) return null
                const variant =
                  unit.ActiveState === 'active'
                    ? 'default'
                    : unit.ActiveState === 'failed'
                      ? 'destructive'
                      : 'secondary'
                return (
                  <Badge variant={variant}>
                    {unit.ActiveState} ({unit.SubState})
                  </Badge>
                )
              })()}
            </DialogTitle>
          </DialogHeader>
          {(journalEntries.length > 0 || searchQuery || hasTimeFilter) && (
            <div className="space-y-2 -mt-2">
              <div className="flex items-center gap-2">
                <div className="relative flex-1 min-w-[120px]">
                  <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                  <Input
                    placeholder="Search logs..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="h-8 pl-7 text-xs"
                  />
                </div>
                <Button
                  variant={hasTimeFilter ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => {
                    if (hasTimeFilter && !timeFilterOpen) {
                      // Active time filter: click clears it.
                      setSinceDate('')
                      setSinceHour('')
                      setUntilDate('')
                      setUntilHour('')
                      setPendingSinceDate('')
                      setPendingSinceHour('')
                      setPendingUntilDate('')
                      setPendingUntilHour('')
                    } else {
                      // Seed pending fields when opening.
                      if (!timeFilterOpen) {
                        setPendingSinceDate(sinceDate)
                        setPendingSinceHour(sinceHour)
                        setPendingUntilDate(untilDate)
                        setPendingUntilHour(untilHour)
                      }
                      setTimeFilterOpen((v) => !v)
                    }
                  }}
                  title={hasTimeFilter && !timeFilterOpen ? 'Clear time filter' : 'Time range filter'}
                >
                  <Clock className="h-3 w-3" />
                </Button>
                <Button
                  variant={followMode ? 'default' : 'outline'}
                  size="sm"
                  onClick={toggleFollow}
                >
                  {followMode ? 'Following' : 'Follow'}
                </Button>
                <Button
                  variant="default"
                  size="sm"
                  onClick={toggleFlatMode}
                >
                  {flatMode ? 'Collapse Tree' : 'Expand Tree'}
                </Button>
                <Button variant="outline" size="sm" onClick={copyJournal}>
                  {copied
                    ? <><ClipboardCheck className="h-3 w-3 mr-1" /> Copied</>
                    : <><Copy className="h-3 w-3 mr-1" /> Copy</>}
                </Button>
              </div>
              {timeFilterOpen && (
                <div className="flex items-center gap-2 flex-wrap pl-1 rounded border bg-muted/50 p-2">
                  <span className="text-xs text-muted-foreground">From</span>
                  <Input
                    type="date"
                    value={pendingSinceDate}
                    onChange={(e) => setPendingSinceDate(e.target.value)}
                    className="h-7 text-xs w-[130px]"
                    title="Start date (defaults to today if hour is set)"
                  />
                  <select
                    value={pendingSinceHour}
                    onChange={(e) => setPendingSinceHour(e.target.value)}
                    className="h-7 text-xs rounded-md border border-input bg-background px-2"
                    title="Start hour (defaults to 00:00 if date is set)"
                  >
                    <option value="">All day</option>
                    {Array.from({ length: 24 }, (_, i) => (
                      <option key={i} value={i}>
                        {String(i).padStart(2, '0')}:00
                      </option>
                    ))}
                  </select>
                  <span className="text-xs text-muted-foreground ml-2">To</span>
                  <Input
                    type="date"
                    value={pendingUntilDate}
                    onChange={(e) => setPendingUntilDate(e.target.value)}
                    className="h-7 text-xs w-[130px]"
                    title="End date (defaults to today if hour is set)"
                  />
                  <select
                    value={pendingUntilHour}
                    onChange={(e) => setPendingUntilHour(e.target.value)}
                    className="h-7 text-xs rounded-md border border-input bg-background px-2"
                    title="End hour (defaults to end of day if date is set)"
                  >
                    <option value="">All day</option>
                    {Array.from({ length: 24 }, (_, i) => (
                      <option key={i} value={i}>
                        {String(i).padStart(2, '0')}:00
                      </option>
                    ))}
                  </select>
                  <Button
                    variant="default"
                    size="sm"
                    className="ml-auto"
                    onClick={() => {
                      setSinceDate(pendingSinceDate)
                      setSinceHour(pendingSinceHour)
                      setUntilDate(pendingUntilDate)
                      setUntilHour(pendingUntilHour)
                      setTimeFilterOpen(false)
                    }}
                  >
                    <Search className="h-3 w-3 mr-1" /> Search
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setPendingSinceDate('')
                      setPendingSinceHour('')
                      setPendingUntilDate('')
                      setPendingUntilHour('')
                      setSinceDate('')
                      setSinceHour('')
                      setUntilDate('')
                      setUntilHour('')
                      setTimeFilterOpen(false)
                    }}
                  >
                    <X className="h-3 w-3 mr-1" /> Clear
                  </Button>
                </div>
              )}
            </div>
          )}
          <div ref={scrollRef} className="overflow-auto flex-1 min-h-0 rounded border bg-muted p-3">
            {journalHasMore && (
              <div className="text-center pb-2">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={journalLoading}
                  onClick={loadMore}
                >
                  {journalLoading ? 'Loading...' : 'Load older entries'}
                </Button>
              </div>
            )}
            <pre className="font-mono text-xs whitespace-pre-wrap break-all" style={{ color: '#888' }}>
              {journalEntries.length === 0 && !journalLoading
                ? 'No journal entries.'
                : flatMode
                  ? journalEntries.map((e, i) => (
                      <div
                        key={e.Cursor || i}
                        className="px-1 py-0.5"
                        style={{ background: i % 2 === 0 ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.05)' }}
                      >
                        <span style={{ color: '#000', background: 'rgba(0,0,0,0.08)', padding: '0 0.3em', borderRadius: '3px', fontWeight: 'bold' }}>
                          {new Date(e.RealtimeTimestamp).toLocaleString()}
                        </span>{' '}
                        {formatMessage(e.Message)}
                      </div>
                    ))
                  : minuteGroups.map((group, gi) => {
                    const expanded = !!expandedGroups[group.key]
                    return (
                      <div key={group.key}>
                        <div
                          className="px-1 py-0.5 cursor-pointer select-none"
                          style={{ background: gi % 2 === 0 ? 'rgba(0,0,0,0.2)' : 'rgba(0,0,0,0.1)' }}
                          onClick={() => toggleGroup(group.key)}
                        >
                          {expanded
                            ? <Triangle className="h-4 w-4 inline-block mr-1 align-middle" fill="currentColor" style={{ transform: 'rotate(180deg)' }} />
                            : <Play className="h-4 w-4 inline-block mr-1 align-middle" fill="currentColor" />}
                          <span style={{ color: '#000', fontWeight: 'bold' }}>{group.label}</span>
                          <span style={{ color: '#333', marginLeft: '0.5em' }}>(count: {group.entries.length})</span>
                          {!expanded && group.entries.length > 0 && (
                            <>{' '}{formatMessage(group.entries[0].Message)}</>
                          )}
                        </div>
                        {expanded && group.entries.map((e, i) => (
                          <div
                            key={e.Cursor || i}
                            className="px-1 py-0.5"
                            style={{ paddingLeft: '1.25rem', background: i % 2 === 0 ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.05)' }}
                          >
                            <span style={{ color: '#000' }}>
                              {new Date(e.RealtimeTimestamp).toLocaleString()}
                            </span>{' '}
                            {formatMessage(e.Message)}
                          </div>
                        ))}
                      </div>
                    )
                  })}
              {journalLoading && journalEntries.length === 0 && (
                <span className="text-muted-foreground animate-pulse">
                  Loading journal...
                </span>
              )}
            </pre>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
