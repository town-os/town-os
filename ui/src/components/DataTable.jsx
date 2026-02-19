import { useState, useMemo } from 'react'
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
 *   columns: { key: string, label: string, transform?: (value: any, record: any) => any, sortable?: boolean }[],
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
}) {
  const [filter, setFilter] = useState('')

  const searchableKeys = useMemo(
    () => columns.filter((c) => c.sortable !== false).map((c) => c.key),
    [columns],
  )

  const serverSide = hasMore !== undefined

  const filtered = useMemo(() => {
    if (!filter) return data
    const term = filter.toLowerCase()
    return data.filter((row) =>
      searchableKeys.some((key) => {
        const val = row[key]
        return val != null && String(val).toLowerCase().includes(term)
      }),
    )
  }, [data, filter, searchableKeys])

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
    const newDirection =
      sortKey === key && sortDirection === 'asc' ? 'desc' : 'asc'
    onSortChange(key, newDirection)
  }

  function handleFilterChange(e) {
    setFilter(e.target.value)
    if (setPage) setPage(0)
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
            if (onReset) onReset()
            if (setPage) setPage(0)
          }}
        >
          <RotateCcw className="h-4 w-4" />
        </Button>
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search..."
            value={filter}
            onChange={handleFilterChange}
            className="pl-8"
          />
        </div>
        <span className="text-sm text-muted-foreground ml-auto">
          {filter && totalFiltered !== totalAll
            ? `${totalFiltered} of ${totalAll} results`
            : `${totalAll} results`}
        </span>
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              {columns.map((col) => (
                <TableHead
                  key={col.key}
                  className={
                    col.sortable !== false && onSortChange
                      ? 'cursor-pointer select-none'
                      : ''
                  }
                  onClick={() =>
                    col.sortable !== false &&
                    onSortChange &&
                    toggleSort(col.key)
                  }
                >
                  <div className="flex items-center gap-1">
                    {col.label}
                    {sortKey === col.key &&
                      (sortDirection === 'asc' ? (
                        <ChevronUp className="h-3 w-3" />
                      ) : (
                        <ChevronDown className="h-3 w-3" />
                      ))}
                  </div>
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {displayed.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="text-center text-muted-foreground py-8"
                >
                  No data
                </TableCell>
              </TableRow>
            ) : (
              displayed.map((row, i) => (
                <TableRow key={row[entryKey] ?? i}>
                  {columns.map((col) => (
                    <TableCell key={col.key}>
                      {col.transform
                        ? col.transform(row[col.key], row)
                        : row[col.key]}
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
            Previous
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
            Next
          </Button>
        </div>
      )}
    </div>
  )
}
