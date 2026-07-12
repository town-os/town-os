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

  it('renders the timestamp as a bare clickable cell, with no trailing clock icon', async () => {
    renderAuditLog()
    // The time cell used to carry a trailing Clock icon button. It sat flush
    // against the Action column, so it read as a stray leading icon on the
    // action text -- the same visual defect the Activity icon caused.
    const timeCell = (await screen.findByText('install package'))
      .closest('tr')
      .querySelector('td:nth-child(2)')
    expect(timeCell.querySelector('svg')).toBeNull()
  })

  it('keeps the timestamp itself as the journal-viewer trigger', async () => {
    renderAuditLog()
    // Removing the icon must not remove the feature: the Clock button was the
    // only entry point to the journal viewer from this table, so the timestamp
    // text now carries the click.
    const timeCell = (await screen.findByText('install package'))
      .closest('tr')
      .querySelector('td:nth-child(2)')
    const trigger = timeCell.querySelector('button')
    expect(trigger).not.toBeNull()
    expect(trigger.textContent).toBe(timeCell.textContent)
  })
})
