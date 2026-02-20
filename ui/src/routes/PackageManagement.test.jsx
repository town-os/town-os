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
  it('renders the Installation Status column header', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installation Status')).toBeTruthy()
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

  it('wraps status badges in tooltip triggers', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const triggers = container.querySelectorAll('[data-slot="tooltip-trigger"]')
    // One tooltip per package row
    expect(triggers.length).toBe(2)
  })

  it('right-aligns the Installation Status column', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installation Status')).toBeTruthy()
    })
    const headers = container.querySelectorAll('th')
    const statusHeader = Array.from(headers).find((th) =>
      th.textContent.includes('Installation Status'),
    )
    expect(statusHeader.className).toContain('text-right')
  })

  it('right-aligns the Installation Status header label', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installation Status')).toBeTruthy()
    })
    const headers = container.querySelectorAll('th')
    const statusHeader = Array.from(headers).find((th) =>
      th.textContent.includes('Installation Status'),
    )
    const innerDiv = statusHeader.querySelector('div')
    expect(innerDiv.className).toContain('justify-end')
  })

  it('right-aligns Installation Status body cells', async () => {
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
})
