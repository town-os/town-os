import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

let mockAuditResponse = { entries: [], has_more: false }

const mockListAuditLog = vi.fn(() => Promise.resolve(mockAuditResponse))

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listAuditLog: mockListAuditLog,
  }),
}))

import AuditLog from './AuditLog.jsx'

function renderAuditLog() {
  return render(
    <MemoryRouter>
      <AuditLog />
    </MemoryRouter>,
  )
}

describe('AuditLog', () => {
  beforeEach(() => {
    mockListAuditLog.mockReset()
    mockAuditResponse = {
      entries: [
        {
          id: 1,
          created_at: '2026-06-14T12:00:00Z',
          action: 'install package',
          path: '/packages/install',
          account: 'admin',
          detail: '',
          success: true,
        },
      ],
      has_more: false,
      total_count: 1,
      total_pages: 1,
    }
    mockListAuditLog.mockImplementation(() => Promise.resolve(mockAuditResponse))
  })

  it('renders the action description as plain text, with no leading icon', async () => {
    renderAuditLog()
    const actionText = await screen.findByText('install package')

    // The action column used to wrap its value in a flex row with a leading
    // Activity icon. The icon added nothing and threw the column's alignment
    // off, so the cell now renders the bare description.
    expect(actionText.closest('span.flex')).toBeNull()
    const cell = actionText.closest('td')
    expect(cell).not.toBeNull()
    expect(cell.querySelector('svg')).toBeNull()
  })
})
