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
          { repo: 'core', name: 'nginx', version: '1.0', installed: true, installed_version: '1.0' },
          { repo: 'core', name: 'redis', version: '7.0', installed: false },
        ],
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
    // One tooltip per package status badge + info icon for installed
    expect(triggers.length).toBe(3)
  })

  it('right-aligns the last column', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const headers = container.querySelectorAll('th')
    const lastHeader = headers[headers.length - 1]
    expect(lastHeader.className).toContain('text-right')
  })

  it('gives all columns equal width', async () => {
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const headers = container.querySelectorAll('th')
    const expectedWidth = `${Math.floor(100 / headers.length)}%`
    for (const th of headers) {
      expect(th.style.width).toBe(expectedWidth)
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
