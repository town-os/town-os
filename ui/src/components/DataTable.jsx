import { useState, useMemo, useEffect, useRef } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ChevronUp, ChevronDown, Search, RotateCcw } from 'lucide-react'

/**
 * @param {{
 *   data: any[],
 *   columns: { key: string, label: string, transform?: (value: any, record: any) => any, sortable?: boolean, width?: string, className?: string, clip?: boolean }[],
 *   entryKey?: string,
 *   page?: number,
 *   setPage?: (n: number) => void,
 *   pageSize?: number,
 *   hasMore?: boolean,
 *   totalPages?: number,
 *   sortKey?: string,
 *   sortDirection?: string,
 *   onSortChange?: (key: string, direction: string) => void,
 *   onReset?: () => void,
 *   onSearchChange?: (search: string) => void,
 *   totalCount?: number,
 * }} props
 */
export default function DataTable({
  data,
  columns,
  entryKey = 'name',
  page,
  setPage,
  pageSize = 20,
  hasMore,
  totalPages: totalPagesProp,
  sortKey,
  sortDirection,
  onSortChange,
  onReset,
  onSearchChange,
  totalCount,
}) {
  const { t } = useI18n()
  const [filter, setFilter] = useState('')
  const debounceRef = useRef(null)
  const onSearchChangeRef = useRef(onSearchChange)
  useEffect(() => {
    onSearchChangeRef.current = onSearchChange
  }, [onSearchChange])

  const searchableKeys = useMemo(
    () => columns.filter((c) => c.sortable !== false).map((c) => c.key),
    [columns],
  )

  const serverSide = hasMore !== undefined

  // In server-side mode with onSearchChange, skip client-side filtering.
  const filtered = useMemo(() => {
    if (serverSide && onSearchChange) return data
    if (!filter) return data
    const term = filter.toLowerCase()
    return data.filter((row) =>
      searchableKeys.some((key) => {
        const val = row[key]
        return val != null && String(val).toLowerCase().includes(term)
      }),
    )
  }, [data, filter, searchableKeys, serverSide, onSearchChange])

  const currentPage = page ?? 0
  const displayed = serverSide
    ? filtered
    : filtered.slice(currentPage * pageSize, (currentPage + 1) * pageSize)

  const totalFiltered = filtered.length
  const totalAll = data.length

  const totalPages = totalPagesProp ?? (Math.ceil(totalFiltered / pageSize) || 1)
  const lastPage = totalPages - 1

  const nextDisabled = serverSide
    ? !hasMore
    : currentPage >= lastPage

  function getPageNumbers() {
    if (totalPages <= 0) return []
    const windowSize = Math.min(10, totalPages)
    const start = Math.max(0, Math.min(currentPage - 4, totalPages - windowSize))
    return Array.from({ length: windowSize }, (_, i) => start + i)
  }

  const pageNumbers = getPageNumbers()

  function toggleSort(key) {
    if (!onSortChange) return
    const col = columns.find((c) => c.key === key)
    if (col && col.sortValues) {
      // Cycle through the defined values for this column.
      const values = col.sortValues
      const idx = values.indexOf(sortDirection)
      const next = idx >= 0 && idx < values.length - 1 ? values[idx + 1] : values[0]
      onSortChange(key, next)
      return
    }
    const newDirection =
      sortKey === key && sortDirection === 'asc' ? 'desc' : 'asc'
    onSortChange(key, newDirection)
  }

  // Debounce server-side search.
  // Use a ref for onSearchChange so this effect only fires when `filter`
  // actually changes — not on every parent re-render (which would create a
  // new callback identity and reset pagination via the debounced handler).
  useEffect(() => {
    if (!onSearchChangeRef.current) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      onSearchChangeRef.current(filter)
    }, 300)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [filter])

  function handleFilterChange(e) {
    setFilter(e.target.value)
    if (!onSearchChange && setPage) setPage(0)
  }

  const lastIdx = columns.length - 1

  // Width allocation: columns with an explicit `width` (e.g. "10%") keep
  // that value; the remaining columns split what's left equally. This
  // lets callers shrink narrow columns (action dropdowns, status badges)
  // so the description / name columns get the rest of the pane instead
  // of every column being forced to the same 25%.
  const totalExplicit = columns.reduce((sum, c) => {
    if (typeof c.width === 'string' && c.width.endsWith('%')) {
      return sum + (parseFloat(c.width) || 0)
    }
    return sum
  }, 0)
  const flexibleCount = columns.filter((c) => !c.width).length
  const flexibleWidth = flexibleCount > 0
    ? `${Math.max(0, Math.floor((100 - totalExplicit) / flexibleCount))}%`
    : undefined

  function colStyle(col) {
    if (columns.length <= 1) return undefined
    if (col.width) return { width: col.width }
    return flexibleWidth ? { width: flexibleWidth } : undefined
  }

  function resolveClassName(col, idx) {
    const parts = []
    if (col.sortable !== false && onSortChange) parts.push('cursor-pointer select-none')
    if (col.className) parts.push(col.className)
    if (idx === lastIdx && !col.className?.includes('text-')) parts.push('text-right')
    return parts.filter(Boolean).join(' ')
  }

  function headerJustify(cls) {
    if (cls?.includes('text-right')) return ' justify-end pr-2'
    if (cls?.includes('text-center')) return ' justify-center'
    return ''
  }

  return (
    <div>
      <div className="flex items-center gap-2 pb-4">
        <Button
          variant="outline"
          size="icon"
          className="h-9 w-9 shrink-0"
          onClick={() => {
            setFilter('')
            if (onSearchChange) onSearchChange('')
            if (onReset) onReset()
            if (setPage) setPage(0)
          }}
        >
          <RotateCcw className="h-4 w-4" />
        </Button>
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder={t('datatable.search_placeholder')}
            value={filter}
            onChange={handleFilterChange}
            className="pl-8"
          />
        </div>
        <span className="text-sm text-muted-foreground ml-auto">
          {serverSide && totalCount != null
            ? `${totalCount} results`
            : filter && totalFiltered !== totalAll
              ? `${totalFiltered} of ${totalAll} results`
              : `${totalAll} results`}
        </span>
      </div>
      <div className="rounded-md border">
        <Table style={{ tableLayout: 'fixed' }}>
          {/* <colgroup> + table-layout:fixed is the only way to make
              column widths binding regardless of cell content. Without
              this, browsers either fall back to equal distribution
              (when the first row spans columns) or treat widths as
              hints (table-layout:auto). */}
          <colgroup>
            {columns.map((col) => (
              <col key={col.key} style={colStyle(col)} />
            ))}
          </colgroup>
          <TableHeader>
            <TableRow>
              {columns.map((col, idx) => {
                const cls = resolveClassName(col, idx)
                return (
                  <TableHead
                    key={col.key}
                    className={cls}
                    style={colStyle(col)}
                    onClick={() =>
                      col.sortable !== false &&
                      onSortChange &&
                      toggleSort(col.key)
                    }
                  >
                    <div className={`flex items-center gap-1${headerJustify(cls)}`}>
                      {/* Header cells are nowrap too, so a label wider than a
                          narrow column would overflow onto the next header
                          exactly as the body cells did. Truncate it, and keep
                          the sort indicator beside it rather than inside the
                          truncating box so it never gets ellipsed away. */}
                      {/* min-w-0: a flex item will not shrink below its content
                          width without it, so truncate alone would do nothing. */}
                      <span className="truncate min-w-0">{col.label}</span>
                      {sortKey === col.key && (
                        col.sortValues
                          ? <span className="text-xs font-normal text-muted-foreground ml-1">({sortDirection})</span>
                          : sortDirection === 'asc'
                            ? <ChevronUp className="h-3 w-3" />
                            : <ChevronDown className="h-3 w-3" />
                      )}
                    </div>
                  </TableHead>
                )
              })}
            </TableRow>
          </TableHeader>
          <TableBody>
            {displayed.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="text-center text-muted-foreground py-8"
                >
                  {t('datatable.no_data')}
                </TableCell>
              </TableRow>
            ) : (
              displayed.map((row, i) => (
                <TableRow key={row[entryKey] ?? i}>
                  {columns.map((col, idx) => (
                    <TableCell
                      key={col.key}
                      className={resolveClassName(col, idx)}
                      style={colStyle(col)}
                    >
                      {/* Cells are whitespace-nowrap and the table is
                          table-layout:fixed, so content wider than its column
                          can neither wrap nor shrink: without something to clip
                          it, it simply paints over the next column. Truncating
                          here rather than on the <td> keeps the overflow on an
                          inner box, so a cell may still host a portalled popover
                          or tooltip without it being clipped away. */}
                      <div className={col.clip === false ? undefined : 'truncate'}>
                        {col.transform
                          ? col.transform(row[col.key], row)
                          : row[col.key]}
                      </div>
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      {setPage && totalPages > 1 && (
        <div className="flex items-center justify-center gap-1 py-4">
          <Button
            variant="outline"
            size="sm"
            disabled={currentPage === 0}
            onClick={() => setPage(Math.max(0, currentPage - 1))}
          >
            {t('datatable.previous')}
          </Button>
          {pageNumbers.map((n) => (
            <Button
              key={n}
              variant="ghost"
              size="sm"
              className={`min-w-[2rem] px-2 ${n === currentPage ? 'font-bold' : 'text-muted-foreground'}`}
              onClick={() => setPage(n)}
            >
              {n + 1}
            </Button>
          ))}
          <Button
            variant="outline"
            size="sm"
            disabled={nextDisabled}
            onClick={() => setPage(currentPage + 1)}
          >
            {t('datatable.next')}
          </Button>
        </div>
      )}
    </div>
  )
}
