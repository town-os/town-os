import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { parseAnsi, parseFields, stripAnsi, groupByMinute } from '@/lib/log-format.js'
import getClient from '@/lib/client-instance.js'
import { useJournalSearch } from '@/lib/use-journal-search.js'
import { useFollowMode } from '@/lib/use-follow-mode.js'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Play,
  X,
  Copy,
  ClipboardCheck,
  Triangle,
  Search,
  Clock,
} from 'lucide-react'

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

export default function JournalViewer({ journalUnit, onClose, units, initialPriority }) {
  const { t } = useI18n()
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
  const [journalPriority, setJournalPriority] = useState(0)

  const minuteGroups = useMemo(() => groupByMinute(journalEntries), [journalEntries])

  function toggleGroup(key) {
    setExpandedGroups((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  function toggleFlatMode() {
    setFlatMode((v) => !v)
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

  const loadEntries = useCallback(async (unitName, beforeCursor, grep, since, until, priority) => {
    setJournalLoading(true)
    const apiUnit = unitName === '__system__' ? '' : unitName
    try {
      const result = await getClient().logTail(apiUnit, 200, beforeCursor, undefined, grep || undefined, since || undefined, until || undefined, priority || undefined)
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
    } catch (err) {
      console.debug('journal load error:', err)
    } finally {
      setJournalLoading(false)
    }
  }, [])

  useEffect(() => {
    if (journalUnit) {
      const p = initialPriority || 0
      setJournalPriority(p)
      loadEntries(journalUnit, undefined, '', undefined, undefined, p)
    }
  }, [journalUnit, initialPriority, loadEntries])

  function loadMore() {
    if (journalUnit && journalCursor) {
      const el = scrollRef.current
      const prevHeight = el?.scrollHeight ?? 0
      loadEntries(journalUnit, journalCursor, searchQuery, undefined, undefined, journalPriority).then(() => {
        requestAnimationFrame(() => {
          if (el) {
            el.scrollTop = el.scrollHeight - prevHeight
          }
        })
      })
    }
  }

  // Re-fetch when search query or since time changes (debounced).
  useJournalSearch(journalUnit, searchQuery, sinceTime, untilTime, loadEntries, journalPriority)

  // Follow mode: poll for new entries.
  useEffect(() => {
    if (!journalUnit || !followMode || !journalEndCursor) return
    const apiUnit = journalUnit === '__system__' ? '' : journalUnit
    const timer = setInterval(async () => {
      try {
        const result = await getClient().logTail(apiUnit, 200, undefined, journalEndCursor, searchQuery || undefined, undefined, undefined, journalPriority || undefined)
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
      } catch (err) {
        console.debug('journal follow error:', err)
      }
    }, 2000)
    return () => clearInterval(timer)
  }, [journalUnit, followMode, journalEndCursor, searchQuery, journalPriority])

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

  return (
    <Dialog open={journalUnit !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-3xl max-h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span>{journalUnit === '__system__' ? (journalPriority > 0 ? t('journal.title_journal_errors') : t('journal.title_system_logs')) : journalUnit?.replace('.service', '')}</span>
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
          <DialogDescription>{t('journal.description')}</DialogDescription>
        </DialogHeader>
        {(journalEntries.length > 0 || searchQuery || hasTimeFilter) && (
          <div className="space-y-2 -mt-2">
            <div className="flex items-center gap-2">
              <div className="relative flex-1 min-w-[120px]">
                <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                <Input
                  placeholder={t('journal.search_placeholder')}
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
                title={hasTimeFilter && !timeFilterOpen ? t('journal.clear_time_filter_tooltip') : t('journal.time_filter_tooltip')}
              >
                <Clock className="h-3 w-3" />
              </Button>
              <Button
                variant={followMode ? 'default' : 'outline'}
                size="sm"
                onClick={toggleFollow}
              >
                {followMode ? t('journal.following_btn') : t('journal.follow_btn')}
              </Button>
              <Button
                variant="default"
                size="sm"
                onClick={toggleFlatMode}
              >
                {flatMode ? t('journal.collapse_tree') : t('journal.expand_tree')}
              </Button>
              <Button variant="outline" size="sm" onClick={copyJournal}>
                {copied
                  ? <><ClipboardCheck className="h-3 w-3 mr-1" /> {t('journal.copied_btn')}</>
                  : <><Copy className="h-3 w-3 mr-1" /> {t('journal.copy_btn')}</>}
              </Button>
            </div>
            {timeFilterOpen && (
              <div className="flex items-center gap-2 flex-wrap pl-1 rounded border bg-muted/50 p-2">
                <span className="text-xs text-muted-foreground">{t('journal.time_from')}</span>
                <Input
                  type="date"
                  value={pendingSinceDate}
                  onChange={(e) => setPendingSinceDate(e.target.value)}
                  className="h-7 text-xs w-[130px]"
                  title={t('journal.start_date_tooltip')}
                />
                <select
                  value={pendingSinceHour}
                  onChange={(e) => setPendingSinceHour(e.target.value)}
                  className="h-7 text-xs rounded-md border border-input bg-background px-2"
                  title={t('journal.start_hour_tooltip')}
                >
                  <option value="">{t('journal.all_day')}</option>
                  {Array.from({ length: 24 }, (_, i) => (
                    <option key={i} value={i}>
                      {String(i).padStart(2, '0')}:00
                    </option>
                  ))}
                </select>
                <span className="text-xs text-muted-foreground ml-2">{t('journal.time_to')}</span>
                <Input
                  type="date"
                  value={pendingUntilDate}
                  onChange={(e) => setPendingUntilDate(e.target.value)}
                  className="h-7 text-xs w-[130px]"
                  title={t('journal.end_date_tooltip')}
                />
                <select
                  value={pendingUntilHour}
                  onChange={(e) => setPendingUntilHour(e.target.value)}
                  className="h-7 text-xs rounded-md border border-input bg-background px-2"
                  title={t('journal.end_hour_tooltip')}
                >
                  <option value="">{t('journal.all_day')}</option>
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
                  <Search className="h-3 w-3 mr-1" /> {t('journal.search_btn')}
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
                  <X className="h-3 w-3 mr-1" /> {t('journal.clear_btn')}
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
                {journalLoading ? t('journal.loading') : t('journal.load_older')}
              </Button>
            </div>
          )}
          <pre className="font-mono text-xs whitespace-pre-wrap break-all" style={{ color: '#888' }}>
            {journalEntries.length === 0 && !journalLoading
              ? t('journal.no_entries')
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
                {t('journal.loading_journal')}
              </span>
            )}
          </pre>
        </div>
      </DialogContent>
    </Dialog>
  )
}
