import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import DataTable from './DataTable.jsx'

describe('DataTable', () => {
  const baseProps = {
    data: [{ name: 'alpha', value: '1' }],
    columns: [
      { key: 'name', label: 'Name' },
      { key: 'value', label: 'Value' },
    ],
    entryKey: 'name',
  }

  it('renders column labels', () => {
    render(<DataTable {...baseProps} />)
    expect(screen.getByText('Name')).toBeTruthy()
    expect(screen.getByText('Value')).toBeTruthy()
  })

  it('renders row data', () => {
    render(<DataTable {...baseProps} />)
    expect(screen.getByText('alpha')).toBeTruthy()
    expect(screen.getByText('1')).toBeTruthy()
  })

  it('applies column className to header cells', () => {
    const columns = [
      { key: 'name', label: 'Name' },
      { key: 'value', label: 'Value', className: 'text-right' },
    ]
    const { container } = render(
      <DataTable {...baseProps} columns={columns} />,
    )
    const headers = container.querySelectorAll('th')
    expect(headers[1].className).toContain('text-right')
  })

  // Cells are whitespace-nowrap inside a table-layout:fixed table, so content
  // too wide for its column can neither wrap nor shrink. With nothing clipping
  // it, it paints straight over the next column -- which is what the audit log
  // looked like: timestamps sitting on top of actions, endpoints on top of users.
  it('clips overlong cell content instead of letting it overflow the column', () => {
    const data = [{ name: 'a'.repeat(200), value: '1' }]
    const { container } = render(<DataTable {...baseProps} data={data} />)

    const cell = container.querySelectorAll('td')[0]
    const box = cell.firstElementChild
    expect(box).toBeTruthy()
    expect(box.className).toContain('truncate')
    expect(box.textContent).toBe('a'.repeat(200))
  })

  // Header cells are nowrap as well, so a label in a deliberately narrow column
  // (the audit log's Detail column is only as wide as its own header) would
  // overflow onto the next header just as the body cells did.
  it('clips a header label that outgrows a narrow column', () => {
    const { container } = render(<DataTable {...baseProps} />)

    const label = container.querySelector('th span')
    expect(label.className).toContain('truncate')
    // Without min-w-0 a flex item refuses to shrink below its content, and
    // truncate would have no effect at all.
    expect(label.className).toContain('min-w-0')
  })

  // A cell that hosts its own overflowing UI (a popover, a menu) can opt out.
  it('leaves a cell unclipped when the column sets clip: false', () => {
    const columns = [
      { key: 'name', label: 'Name', clip: false },
      { key: 'value', label: 'Value' },
    ]
    const { container } = render(<DataTable {...baseProps} columns={columns} />)

    const cells = container.querySelectorAll('td')
    expect(cells[0].firstElementChild.className).not.toContain('truncate')
    expect(cells[1].firstElementChild.className).toContain('truncate')
  })

  // Fixed layout splits the pane equally by default, which starves the columns
  // with the longest content and hands a full share to one holding an icon.
  it('binds an explicit column width to its <col>', () => {
    const columns = [
      { key: 'name', label: 'Name', width: '80%' },
      { key: 'value', label: 'Value', width: '20%' },
    ]
    const { container } = render(<DataTable {...baseProps} columns={columns} />)

    const cols = container.querySelectorAll('colgroup col')
    expect(cols[0].style.width).toBe('80%')
    expect(cols[1].style.width).toBe('20%')
  })

  it('applies column className to body cells', () => {
    const columns = [
      { key: 'name', label: 'Name' },
      { key: 'value', label: 'Value', className: 'text-right' },
    ]
    const { container } = render(
      <DataTable {...baseProps} columns={columns} />,
    )
    const cells = container.querySelectorAll('td')
    expect(cells[1].className).toContain('text-right')
  })

  it('adds justify-end to header div when column has text-right', () => {
    const columns = [
      { key: 'name', label: 'Name' },
      { key: 'value', label: 'Value', className: 'text-right' },
    ]
    const { container } = render(
      <DataTable
        {...baseProps}
        columns={columns}
        sortKey="name"
        sortDirection="asc"
        onSortChange={vi.fn()}
      />,
    )
    const headerDivs = container.querySelectorAll('th div')
    expect(headerDivs[0].className).not.toContain('justify-end')
    expect(headerDivs[1].className).toContain('justify-end')
  })

  it('auto-applies text-right and justify-end to last column', () => {
    const { container } = render(
      <DataTable
        {...baseProps}
        sortKey="name"
        sortDirection="asc"
        onSortChange={vi.fn()}
      />,
    )
    const headerDivs = container.querySelectorAll('th div')
    expect(headerDivs[0].className).not.toContain('justify-end')
    // Last column automatically gets text-right alignment
    expect(headerDivs[1].className).toContain('justify-end')
  })

  it('shows "No data" when data is empty', () => {
    render(<DataTable {...baseProps} data={[]} />)
    expect(screen.getByText('No data')).toBeTruthy()
  })

  it('applies transform function to cell values', () => {
    const columns = [
      { key: 'name', label: 'Name', transform: (v) => `[${v}]` },
    ]
    render(<DataTable {...baseProps} columns={columns} />)
    expect(screen.getByText('[alpha]')).toBeTruthy()
  })

  it('displays totalCount in server-side mode', () => {
    render(
      <DataTable
        {...baseProps}
        hasMore={false}
        totalPages={5}
        totalCount={42}
      />,
    )
    expect(screen.getByText('42 results')).toBeTruthy()
  })

  it('displays data.length in client-side mode', () => {
    const data = [
      { name: 'alpha', value: '1' },
      { name: 'beta', value: '2' },
    ]
    render(<DataTable {...baseProps} data={data} />)
    expect(screen.getByText('2 results')).toBeTruthy()
  })

  describe('onSearchChange debounce', () => {
    beforeEach(() => { vi.useFakeTimers() })
    afterEach(() => { vi.useRealTimers() })

    it('does not fire onSearchChange when parent re-renders without filter change', () => {
      const onSearch1 = vi.fn()
      const serverProps = {
        ...baseProps,
        hasMore: false,
        totalPages: 1,
        page: 0,
        setPage: vi.fn(),
        sortKey: 'name',
        sortDirection: 'asc',
        onSortChange: vi.fn(),
      }

      const { rerender } = render(
        <DataTable {...serverProps} onSearchChange={onSearch1} />,
      )

      // Flush the initial mount debounce
      act(() => { vi.advanceTimersByTime(400) })
      onSearch1.mockClear()

      // Re-render with a NEW onSearchChange reference (simulates parent
      // re-rendering from a polling update — inline callbacks get new identity)
      const onSearch2 = vi.fn()
      rerender(
        <DataTable {...serverProps} onSearchChange={onSearch2} />,
      )

      // Advance well past the 300ms debounce window
      act(() => { vi.advanceTimersByTime(500) })

      // Neither callback should have been invoked since filter didn't change
      expect(onSearch1).not.toHaveBeenCalled()
      expect(onSearch2).not.toHaveBeenCalled()
    })

    it('fires onSearchChange when user types in the search box', () => {
      const onSearch = vi.fn()
      const serverProps = {
        ...baseProps,
        hasMore: false,
        totalPages: 1,
        page: 0,
        setPage: vi.fn(),
        sortKey: 'name',
        sortDirection: 'asc',
        onSortChange: vi.fn(),
        onSearchChange: onSearch,
      }

      render(<DataTable {...serverProps} />)

      // Flush mount debounce
      act(() => { vi.advanceTimersByTime(400) })
      onSearch.mockClear()

      // Type in the search box
      const input = screen.getByPlaceholderText('Search...')
      fireEvent.change(input, { target: { value: 'test' } })

      // Before debounce expires — not called yet
      act(() => { vi.advanceTimersByTime(200) })
      expect(onSearch).not.toHaveBeenCalled()

      // After debounce expires — called with the typed value
      act(() => { vi.advanceTimersByTime(200) })
      expect(onSearch).toHaveBeenCalledWith('test')
    })
  })
})
