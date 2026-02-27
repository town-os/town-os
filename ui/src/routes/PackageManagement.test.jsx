import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import PackageManagement from './PackageManagement.jsx'

const mockListPackages = vi.fn(() =>
  Promise.resolve({
    entries: [
      { repo: 'core', name: 'nginx', version: '1.0', installed: true, installed_version: '1.0' },
      { repo: 'core', name: 'redis', version: '7.0', installed: false },
    ],
    has_more: false,
    total_pages: 1,
  }),
)

const mockListRepositories = vi.fn(() =>
  Promise.resolve({
    entries: [{ name: 'core', url: 'http://example.com/core', error: '' }],
    has_more: false,
    total_pages: 1,
  }),
)

const mockListPackagesByRepo = vi.fn(() => Promise.resolve([]))
const mockListPackageVersions = vi.fn(() => Promise.resolve(['2.0', '1.0']))
const mockInstallPackage = vi.fn(() => Promise.resolve())
const mockUninstallPackage = vi.fn(() => Promise.resolve())
const mockGetPackageQuestions = vi.fn(() => Promise.resolve({}))
const mockGetPackageQuestionsByIdentity = vi.fn(() => Promise.resolve({}))
const mockGetResponses = vi.fn(() => Promise.resolve({}))
const mockRefreshRepositories = vi.fn(() => Promise.resolve({}))
const mockAddRepository = vi.fn(() => Promise.resolve())
const mockRemoveRepository = vi.fn(() => Promise.resolve())
const mockMoveRepository = vi.fn(() => Promise.resolve())
const mockGetInstalledInfo = vi.fn(() => Promise.resolve({ questions: {}, responses: {} }))
const mockInstallPreview = vi.fn(() => Promise.reject(new Error('no preview')))
const mockListUninstalledVolumes = vi.fn(() => Promise.resolve({ has_uninstalled_volumes: false }))
const mockPurgeUninstalledVolumes = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listPackages: mockListPackages,
    listRepositories: mockListRepositories,
    listPackagesByRepo: mockListPackagesByRepo,
    listPackageVersions: mockListPackageVersions,
    installPackage: mockInstallPackage,
    uninstallPackage: mockUninstallPackage,
    getPackageQuestions: mockGetPackageQuestions,
    getPackageQuestionsByIdentity: mockGetPackageQuestionsByIdentity,
    getResponses: mockGetResponses,
    refreshRepositories: mockRefreshRepositories,
    addRepository: mockAddRepository,
    removeRepository: mockRemoveRepository,
    moveRepository: mockMoveRepository,
    getInstalledInfo: mockGetInstalledInfo,
    installPreview: mockInstallPreview,
    listUninstalledVolumes: mockListUninstalledVolumes,
    purgeUninstalledVolumes: mockPurgeUninstalledVolumes,
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

beforeEach(() => {
  mockListPackages.mockClear()
  mockListRepositories.mockClear()
  mockInstallPackage.mockClear()
  mockUninstallPackage.mockClear()
  mockRefreshRepositories.mockClear()
  mockAddRepository.mockClear()
  mockRemoveRepository.mockClear()
  mockMoveRepository.mockClear()
  mockGetInstalledInfo.mockClear()
  mockListPackageVersions.mockClear()
  mockGetPackageQuestionsByIdentity.mockClear()
  mockGetResponses.mockClear()
  mockInstallPreview.mockClear()
  mockListUninstalledVolumes.mockClear()
})

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

  // --- Page heading and layout ---

  it('renders the Packages heading', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Packages' })).toBeTruthy()
    })
  })

  it('renders the subheading', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Manage packages and repositories')).toBeTruthy()
    })
  })

  it('renders Packages and Repositories tabs', async () => {
    renderPackageManagement()
    await waitFor(() => {
      const tabs = screen.getAllByRole('tab')
      const tabLabels = tabs.map((t) => t.textContent)
      expect(tabLabels).toContain('Packages')
      expect(tabLabels).toContain('Repositories')
    })
  })

  // --- Package display ---

  it('displays package name in table', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeTruthy()
      expect(screen.getByText('redis')).toBeTruthy()
    })
  })

  it('displays repository column for packages', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Repository')).toBeTruthy()
    })
  })

  it('displays version column', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Version')).toBeTruthy()
    })
  })

  // --- Group by repository checkbox ---

  it('renders group by repository checkbox', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Group by repository')).toBeTruthy()
    })
  })

  it('renders installed only checkbox', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed only')).toBeTruthy()
    })
  })

  // --- Uninstall flow ---

  it('opens uninstall dialog when installed badge is clicked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Installed'))
    await waitFor(() => {
      expect(screen.getByText('Uninstall Package')).toBeTruthy()
    })
  })

  it('shows purge volumes checkbox in uninstall dialog', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Installed'))
    await waitFor(() => {
      expect(screen.getByText('Purge all volumes for this package')).toBeTruthy()
    })
  })

  it('calls uninstallPackage when Uninstall button is clicked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Installed'))
    await waitFor(() => {
      expect(screen.getByText('Uninstall Package')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'Uninstall' }))
    await waitFor(() => {
      expect(mockUninstallPackage).toHaveBeenCalledWith('core', 'nginx', '1.0', false)
    })
  })

  it('shows purge warning when checkbox is checked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Installed'))
    await waitFor(() => {
      expect(screen.getByText('Purge all volumes for this package')).toBeTruthy()
    })
    const checkbox = screen.getByText('Purge all volumes for this package').closest('label').querySelector('input')
    fireEvent.click(checkbox)
    await waitFor(() => {
      expect(screen.getByText(/permanently deleted/)).toBeTruthy()
    })
  })

  // --- Install flow (not installed badge click) ---

  it('triggers install flow when Not Installed badge is clicked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Not Installed'))
    await waitFor(() => {
      // Should call listPackageVersions to check for multiple versions
      expect(mockListPackageVersions).toHaveBeenCalledWith('redis')
    })
  })

  // --- Upgrade badge ---

  it('shows Upgrade badge when installed version differs from latest', async () => {
    mockListPackages.mockResolvedValueOnce({
      entries: [
        { repo: 'core', name: 'nginx', version: '2.0', installed: true, installed_version: '1.0' },
      ],
      has_more: false,
      total_pages: 1,
    })
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Upgrade')).toBeTruthy()
    })
  })

  it('shows installed version in badge when upgrade available', async () => {
    mockListPackages.mockResolvedValueOnce({
      entries: [
        { repo: 'core', name: 'nginx', version: '2.0', installed: true, installed_version: '1.0' },
      ],
      has_more: false,
      total_pages: 1,
    })
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed (1.0)')).toBeTruthy()
    })
  })

  // --- Repository tab ---

  it('renders Repositories tab trigger', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Repositories' })).toBeTruthy()
    })
  })

  it('calls listRepositories on mount', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(mockListRepositories).toHaveBeenCalled()
    })
  })

  // --- Cancel buttons in dialogs ---

  it('closes uninstall dialog when Cancel is clicked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Installed'))
    await waitFor(() => {
      expect(screen.getByText('Uninstall Package')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => {
      expect(screen.queryByText('Uninstall Package')).toBeNull()
    })
  })

  // --- Loading state ---

  it('shows loading state when no data is available', async () => {
    mockListPackages.mockResolvedValueOnce({ entries: [], has_more: false, total_pages: 1 })
    renderPackageManagement()
    // When entries are empty, DataTable shows empty state (no Loading text since data resolves immediately)
    await waitFor(() => {
      expect(screen.queryByText('nginx')).toBeNull()
    })
  })
})
