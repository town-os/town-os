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
