import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import PackageManagement from './PackageManagement.jsx'

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listPackages: vi.fn(() =>
      Promise.resolve({
        entries: [
          { name: 'nginx', version: '1.0' },
          { name: 'redis', version: '7.0' },
        ],
        has_more: false,
        total_pages: 1,
      }),
    ),
    listInstalled: vi.fn(() =>
      Promise.resolve({
        entries: ['nginx@1.0'],
        has_more: false,
        total_pages: 1,
      }),
    ),
    listRepositories: vi.fn(() =>
      Promise.resolve({
        entries: [{ name: 'core', url: 'http://example.com/core', error: '' }],
        has_more: false,
        total_pages: 1,
      }),
    ),
    getPackageQuestions: vi.fn(() => Promise.resolve({})),
    installPackage: vi.fn(() => Promise.resolve()),
  }),
}))

function renderPackageManagement() {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <PackageManagement />
      </TooltipProvider>
    </MemoryRouter>,
  )
}

describe('PackageManagement', () => {
  it('renders the Status column header', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Status')).toBeTruthy()
    })
  })

  it('renders installed badge for installed package', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
  })

  it('renders not installed badge for uninstalled package', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })
  })

  it('wraps status badges and info icon in tooltip triggers', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const triggers = container.querySelectorAll('[data-slot="tooltip-trigger"]')
    // One tooltip per package status badge + one info icon for installed row
    expect(triggers.length).toBe(3)
  })

  it('right-aligns the Status column', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const headers = container.querySelectorAll('th')
    const statusHeader = Array.from(headers).find((th) =>
      th.textContent.includes('Status'),
    )
    expect(statusHeader.className).toContain('text-right')
  })

  it('right-aligns the Status header label', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const headers = container.querySelectorAll('th')
    const statusHeader = Array.from(headers).find((th) =>
      th.textContent.includes('Status'),
    )
    const innerDiv = statusHeader.querySelector('div')
    expect(innerDiv.className).toContain('justify-end')
  })

  it('right-aligns Status body cells', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const rows = container.querySelectorAll('tbody tr')
    for (const row of rows) {
      const cells = row.querySelectorAll('td')
      const lastCell = cells[cells.length - 1]
      expect(lastCell.className).toContain('text-right')
    }
  })

  it('shows info icon only for installed packages', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    // Info icon uses lucide Info which renders as an svg
    const rows = container.querySelectorAll('tbody tr')
    // First row (nginx) is installed — should have info button
    const nginxInfoBtn = rows[0].querySelector('button svg.lucide-info')
    expect(nginxInfoBtn).toBeTruthy()
    // Second row (redis) is not installed — should not have info button
    const redisInfoBtn = rows[1].querySelector('button svg.lucide-info')
    expect(redisInfoBtn).toBeNull()
  })
})
