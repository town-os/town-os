import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
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

  it('does not add justify-end when column has no className', () => {
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
    expect(headerDivs[1].className).not.toContain('justify-end')
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
})
