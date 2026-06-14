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

  it('renders the action description with a leading icon that pushes the text right', async () => {
    renderAuditLog()
    const actionText = await screen.findByText('install package')

    // The action text is wrapped in a flex row alongside the icon, so the
    // icon occupies its own horizontal space and the description sits to its
    // right (rather than the icon being hidden under the text).
    const row = actionText.closest('span.flex')
    expect(row).not.toBeNull()
    expect(row.className).toContain('items-center')
    expect(row.className).toContain('gap-2')

    // A leading lucide icon precedes the text within that row.
    const icon = row.querySelector('svg.lucide-activity')
    expect(icon).not.toBeNull()
    expect(icon.classList.contains('shrink-0')).toBe(true)
  })
})
