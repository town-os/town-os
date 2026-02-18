import { useState } from 'react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { ChevronUp, ChevronDown } from 'lucide-react'

/**
 * @param {{
 *   data: any[],
 *   columns: { key: string, label: string, transform?: (value: any, record: any) => any, sortable?: boolean }[],
 *   entryKey?: string,
 *   page?: number,
 *   setPage?: (n: number) => void,
 *   pageSize?: number,
 * }} props
 */
export default function DataTable({
  data,
  columns,
  entryKey = 'name',
  page,
  setPage,
  pageSize = 20,
}) {
  const [sortConfig, setSortConfig] = useState({
    key: columns[0]?.key,
    direction: 'asc',
  })

  const sorted = [...data].sort((a, b) => {
    const aVal = a[sortConfig.key]
    const bVal = b[sortConfig.key]
    if (aVal == null || bVal == null) return 0
    const cmp =
      typeof aVal === 'string'
        ? aVal.localeCompare(bVal, undefined, { sensitivity: 'base' })
        : aVal > bVal
          ? 1
          : aVal < bVal
            ? -1
            : 0
    return sortConfig.direction === 'asc' ? cmp : -cmp
  })

  function toggleSort(key) {
    setSortConfig((prev) =>
      prev.key === key
        ? { key, direction: prev.direction === 'asc' ? 'desc' : 'asc' }
        : { key, direction: 'asc' },
    )
  }

  return (
    <div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              {columns.map((col) => (
                <TableHead
                  key={col.key}
                  className={
                    col.sortable !== false ? 'cursor-pointer select-none' : ''
                  }
                  onClick={() => col.sortable !== false && toggleSort(col.key)}
                >
                  <div className="flex items-center gap-1">
                    {col.label}
                    {sortConfig.key === col.key &&
                      (sortConfig.direction === 'asc' ? (
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
            {sorted.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="text-center text-muted-foreground py-8"
                >
                  No data
                </TableCell>
              </TableRow>
            ) : (
              sorted.map((row, i) => (
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
      {page !== undefined && setPage && (
        <div className="flex items-center justify-between py-4">
          <Button
            variant="outline"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage(Math.max(0, page - 1))}
          >
            Previous
          </Button>
          <span className="text-sm text-muted-foreground">
            Page {page + 1}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={data.length < pageSize}
            onClick={() => setPage(page + 1)}
          >
            Next
          </Button>
        </div>
      )}
    </div>
  )
}
