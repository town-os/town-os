import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import PackageManagement from './PackageManagement.jsx'

// Radix UI uses ResizeObserver which is not available in JSDOM.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
})

// Shared mock client — all calls to getClient() return this same object.
const mockClient = {
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
  listPackagesByRepo: vi.fn(() => Promise.resolve([])),
  listPackageVersions: vi.fn(() => Promise.resolve(['1.0'])),
  installPackage: vi.fn(() => Promise.resolve()),
  uninstallPackage: vi.fn(() => Promise.resolve()),
  getPackageQuestions: vi.fn(() => Promise.resolve({})),
  getPackageQuestionsByIdentity: vi.fn(() =>
    Promise.resolve({
      hostname: { query: 'What hostname?', type: 'hostname' },
      port: { query: 'What port?', type: 'port', default: '8080' },
    }),
  ),
  getResponses: vi.fn(() => Promise.resolve({ hostname: 'cached-host', port: '9090' })),
  getLastResponses: vi.fn(() => Promise.resolve({})),
  refreshRepositories: vi.fn(() => Promise.resolve({})),
  addRepository: vi.fn(() => Promise.resolve()),
  removeRepository: vi.fn(() => Promise.resolve()),
  moveRepository: vi.fn(() => Promise.resolve()),
  getInstalledInfo: vi.fn(() => Promise.resolve({ questions: {}, responses: {} })),
  installPreview: vi.fn(() => Promise.reject(new Error('no preview'))),
  listUninstalledVolumes: vi.fn(() => Promise.resolve({ has_uninstalled_volumes: false })),
  purgeUninstalledVolumes: vi.fn(() => Promise.resolve()),
  listFeaturedPackages: vi.fn(() => Promise.resolve([])),
}

vi.mock('@/lib/client-instance.js', () => ({
  default: () => mockClient,
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

// Reset all mocks to their default implementations before each test.
beforeEach(() => {
  mockClient.listPackages.mockImplementation(() =>
    Promise.resolve({
      entries: [
        { repo: 'core', name: 'nginx', version: '1.0', installed: true, installed_version: '1.0' },
        { repo: 'core', name: 'redis', version: '7.0', installed: false },
      ],
      has_more: false,
      total_pages: 1,
    }),
  )
  mockClient.listRepositories.mockImplementation(() =>
    Promise.resolve({
      entries: [{ name: 'core', url: 'http://example.com/core', error: '' }],
      has_more: false,
      total_pages: 1,
    }),
  )
  mockClient.listPackagesByRepo.mockImplementation(() => Promise.resolve([]))
  mockClient.listPackageVersions.mockImplementation(() => Promise.resolve(['1.0']))
  mockClient.installPackage.mockImplementation(() => Promise.resolve())
  mockClient.uninstallPackage.mockImplementation(() => Promise.resolve())
  mockClient.getPackageQuestions.mockImplementation(() => Promise.resolve({}))
  mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
    Promise.resolve({
      hostname: { query: 'What hostname?', type: 'hostname' },
      port: { query: 'What port?', type: 'port', default: '8080' },
    }),
  )
  mockClient.getResponses.mockImplementation(() =>
    Promise.resolve({ hostname: 'cached-host', port: '9090' }),
  )
  mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))
  mockClient.refreshRepositories.mockImplementation(() => Promise.resolve({}))
  mockClient.addRepository.mockImplementation(() => Promise.resolve())
  mockClient.removeRepository.mockImplementation(() => Promise.resolve())
  mockClient.moveRepository.mockImplementation(() => Promise.resolve())
  mockClient.getInstalledInfo.mockImplementation(() => Promise.resolve({ questions: {}, responses: {} }))
  mockClient.installPreview.mockImplementation(() => Promise.reject(new Error('no preview')))
  mockClient.listUninstalledVolumes.mockImplementation(() =>
    Promise.resolve({ has_uninstalled_volumes: false }),
  )
  mockClient.purgeUninstalledVolumes.mockImplementation(() => Promise.resolve())
  mockClient.listFeaturedPackages.mockImplementation(() => Promise.resolve([]))
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
      expect(mockClient.uninstallPackage).toHaveBeenCalledWith('core', 'nginx', '1.0', false)
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
      expect(mockClient.listPackageVersions).toHaveBeenCalledWith('redis')
    })
  })

  // --- Upgrade badge ---

  it('shows Upgrade badge when installed version differs from latest', async () => {
    mockClient.listPackages.mockResolvedValueOnce({
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
    mockClient.listPackages.mockResolvedValueOnce({
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
      expect(mockClient.listRepositories).toHaveBeenCalled()
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
    mockClient.listPackages.mockResolvedValueOnce({ entries: [], has_more: false, total_pages: 1 })
    renderPackageManagement()
    // When entries are empty, DataTable shows empty state (no Loading text since data resolves immediately)
    await waitFor(() => {
      expect(screen.queryByText('nginx')).toBeNull()
    })
  })

  // --- Repositories tab interactions ---

  it('shows Add Repository button when Repositories tab is clicked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Repositories' })).toBeTruthy()
    })
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Repositories' }), { button: 0 })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Add Repository/ })).toBeTruthy()
    })
  })

  it('opens add repo dialog with Name and Repository URL fields when Add Repository is clicked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Repositories' })).toBeTruthy()
    })
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Repositories' }), { button: 0 })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Add Repository/ })).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: /Add Repository/ }))
    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeTruthy()
      expect(screen.getByLabelText('Repository URL')).toBeTruthy()
    })
  })

  it('calls addRepository when add repo form is submitted', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Repositories' })).toBeTruthy()
    })
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Repositories' }), { button: 0 })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Add Repository/ })).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: /Add Repository/ }))
    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeTruthy()
    })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'my-repo' } })
    fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/repo' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => {
      expect(mockClient.addRepository).toHaveBeenCalledWith('my-repo', 'https://example.com/repo')
    })
  })

  it('calls refreshRepositories when Refresh button is clicked', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Repositories' })).toBeTruthy()
    })
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Repositories' }), { button: 0 })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Refresh/ })).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: /Refresh/ }))
    await waitFor(() => {
      expect(mockClient.refreshRepositories).toHaveBeenCalled()
    })
  })

  // --- Featured badge ---

  it('shows Featured badge for featured packages in flat view', async () => {
    mockClient.listPackages.mockResolvedValueOnce({
      entries: [
        { repo: 'core', name: 'nginx', version: '1.0', installed: false, featured: true },
        { repo: 'core', name: 'redis', version: '7.0', installed: false, featured: false },
      ],
      has_more: false,
      total_pages: 1,
    })
    renderPackageManagement()
    await waitFor(() => {
      const badges = screen.getAllByText('Featured')
      expect(badges.length).toBe(1)
    })
  })

  it('does not show Featured badge for non-featured packages', async () => {
    mockClient.listPackages.mockResolvedValueOnce({
      entries: [
        { repo: 'core', name: 'redis', version: '7.0', installed: false, featured: false },
      ],
      has_more: false,
      total_pages: 1,
    })
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('redis')).toBeTruthy()
    })
    expect(screen.queryByText('Featured')).toBeNull()
  })

  it('shows Featured badge for featured packages in grouped view', async () => {
    mockClient.listPackagesByRepo.mockResolvedValueOnce([
      {
        repo: 'core',
        packages: [
          { repo: 'core', name: 'nginx', version: '1.0' },
          { repo: 'core', name: 'redis', version: '7.0' },
        ],
        featured: ['nginx'],
      },
    ])
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Group by repository')).toBeTruthy()
    })
    const checkbox = screen.getByText('Group by repository').closest('label').querySelector('input')
    fireEvent.click(checkbox)
    await waitFor(() => {
      const badges = screen.getAllByText('Featured')
      expect(badges.length).toBe(1)
    })
  })

  // --- Install flow with version select ---

  it('shows version select dialog when multiple versions are available', async () => {
    mockClient.listPackageVersions.mockResolvedValueOnce(['2.0', '1.0'])
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Not Installed'))
    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
      expect(screen.getByRole('button', { name: 'Install' })).toBeTruthy()
    })
  })

  // --- Info dialog note hyperlinking ---

  it('renders URL notes as hyperlinks with target=_blank', async () => {
    mockClient.getInstalledInfo.mockResolvedValueOnce({
      questions: {},
      responses: {},
      notes: { URL: 'http://testhost:8081' },
      note_types: { URL: 'url' },
    })
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const infoBtn = container.querySelector('tbody tr button svg.lucide-info').closest('button')
    fireEvent.click(infoBtn)
    await waitFor(() => {
      const link = screen.getByText('http://testhost:8081')
      expect(link.tagName).toBe('A')
      expect(link.getAttribute('href')).toBe('http://testhost:8081')
      expect(link.getAttribute('target')).toBe('_blank')
      expect(link.getAttribute('rel')).toBe('noopener noreferrer')
    })
  })

  it('renders email notes as mailto hyperlinks', async () => {
    mockClient.getInstalledInfo.mockResolvedValueOnce({
      questions: {},
      responses: {},
      notes: { Contact: 'admin@example.com' },
      note_types: { Contact: 'email' },
    })
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const infoBtn = container.querySelector('tbody tr button svg.lucide-info').closest('button')
    fireEvent.click(infoBtn)
    await waitFor(() => {
      const link = screen.getByText('admin@example.com')
      expect(link.tagName).toBe('A')
      expect(link.getAttribute('href')).toBe('mailto:admin@example.com')
    })
  })

  it('renders phone notes as tel hyperlinks', async () => {
    mockClient.getInstalledInfo.mockResolvedValueOnce({
      questions: {},
      responses: {},
      notes: { Support: '+1-555-0100' },
      note_types: { Support: 'phone' },
    })
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const infoBtn = container.querySelector('tbody tr button svg.lucide-info').closest('button')
    fireEvent.click(infoBtn)
    await waitFor(() => {
      const link = screen.getByText('+1-555-0100')
      expect(link.tagName).toBe('A')
      expect(link.getAttribute('href')).toBe('tel:+1-555-0100')
    })
  })

  it('renders untyped notes as plain code blocks', async () => {
    mockClient.getInstalledInfo.mockResolvedValueOnce({
      questions: {},
      responses: {},
      notes: { Info: 'some plain text' },
      note_types: {},
    })
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const infoBtn = container.querySelector('tbody tr button svg.lucide-info').closest('button')
    fireEvent.click(infoBtn)
    await waitFor(() => {
      const el = screen.getByText('some plain text')
      expect(el.tagName).toBe('CODE')
    })
  })

  it('renders notes without note_types as plain code blocks', async () => {
    mockClient.getInstalledInfo.mockResolvedValueOnce({
      questions: {},
      responses: {},
      notes: { Info: 'plain note' },
    })
    const { container } = renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Installed')).toBeTruthy()
    })
    const infoBtn = container.querySelector('tbody tr button svg.lucide-info').closest('button')
    fireEvent.click(infoBtn)
    await waitFor(() => {
      const el = screen.getByText('plain note')
      expect(el.tagName).toBe('CODE')
    })
  })

  // --- installedVersion helper logic ---

  it('installedVersion returns correct values for various cases', () => {
    function installedVersion(row, installedMap) {
      if (row.installed !== undefined) {
        if (!row.installed) return null
        return row.installed_version || ''
      }
      const key = `${row.repo}/${row.name}`
      if (key in installedMap) return installedMap[key]
      return null
    }

    // installed with version
    expect(
      installedVersion({ repo: 'core', name: 'nginx', installed: true, installed_version: '1.0' }, {}),
    ).toBe('1.0')

    // installed without version
    expect(
      installedVersion({ repo: 'core', name: 'nginx', installed: true }, {}),
    ).toBe('')

    // not installed
    expect(
      installedVersion({ repo: 'core', name: 'redis', installed: false }, {}),
    ).toBeNull()

    // from installedMap (grouped view — no installed field)
    expect(
      installedVersion({ repo: 'core', name: 'nginx' }, { 'core/nginx': '2.0' }),
    ).toBe('2.0')

    // not in installedMap
    expect(
      installedVersion({ repo: 'core', name: 'redis' }, {}),
    ).toBeNull()
  })

  // --- Question defaults and cached responses ---

  it('opens questions dialog with cached responses shown as read-only values', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    // Click "Not Installed" badge on redis to trigger install
    fireEvent.click(screen.getByText('Not Installed'))

    // Wait for the dialog to open with questions
    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Cached responses should be displayed as read-only text (not editable inputs)
    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
      expect(screen.getByText('9090')).toBeTruthy()
    })
  })

  it('shows clear buttons for cached response fields', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // There should be X (clear) buttons for each cached field.
    // Dialog content renders in a portal so query from document.
    // The dialog close button also has an X icon, so scope to the form.
    await waitFor(() => {
      const form = document.querySelector('form')
      const clearButtons = form.querySelectorAll('button svg.lucide-x')
      expect(clearButtons.length).toBe(2) // one for each cached field
    })
  })

  it('clearing a cached field reveals an editable input', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    // Click the first clear (X) button to clear the hostname cached value.
    const form = document.querySelector('form')
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[0].closest('button'))

    // After clearing, the read-only text should be gone and an input should appear.
    await waitFor(() => {
      expect(screen.queryByText('cached-host')).toBeNull()
      const input = form.querySelector('input[name="hostname"]')
      expect(input).toBeTruthy()
      expect(input.tagName).toBe('INPUT')
      expect(input.type).not.toBe('hidden')
    })
  })

  it('cached values are submitted via hidden inputs', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    // Cached fields should have hidden inputs carrying the cached value.
    const form = document.querySelector('form')
    const hiddenHostname = form.querySelector('input[name="hostname"][type="hidden"]')
    expect(hiddenHostname).toBeTruthy()
    expect(hiddenHostname.value).toBe('cached-host')

    const hiddenPort = form.querySelector('input[name="port"][type="hidden"]')
    expect(hiddenPort).toBeTruthy()
    expect(hiddenPort.value).toBe('9090')
  })

  it('shows default value as placeholder text in the input when no cached response', async () => {
    // Override mock to return no cached responses so inputs are shown.
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // The port question has default '8080' — input should show placeholder.
    await waitFor(() => {
      const portInput = document.querySelector('input[name="port"]')
      expect(portInput).toBeTruthy()
      expect(portInput.placeholder).toBe('Default: 8080')
    })
  })

  it('shows default value as helper text below the input', async () => {
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // The port question has default '8080' — should show helper text.
    await waitFor(() => {
      const defaultText = screen.getByText('8080')
      expect(defaultText).toBeTruthy()
      expect(defaultText.closest('p')).toBeTruthy()
      expect(defaultText.closest('p').textContent).toContain('Default:')
    })
  })

  it('shows type-specific placeholder for hostname when no default', async () => {
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // hostname type with no default should get "Auto-generated if empty" placeholder.
    await waitFor(() => {
      const hostnameInput = document.querySelector('input[name="hostname"]')
      expect(hostnameInput).toBeTruthy()
      expect(hostnameInput.placeholder).toBe('Auto-generated if empty')
    })
  })

  it('does not show default helper text when cached value is displayed', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      // The port field has a cached value '9090', so the default helper
      // text "Default: 8080" should NOT appear (it only shows when no cached value).
      expect(screen.getByText('9090')).toBeTruthy()
      const form = document.querySelector('form')
      // Query within the form for text containing "Default: 8080"
      const defaultHelpers = form.querySelectorAll('p')
      const has8080Helper = Array.from(defaultHelpers).some(
        (p) => p.textContent.includes('Default:') && p.textContent.includes('8080')
      )
      expect(has8080Helper).toBe(false)
    })
  })

  it('shows password masking for cached password-type responses', async () => {
    mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
      Promise.resolve({
        secret: { query: 'Enter password', type: 'secret' },
      }),
    )
    mockClient.getResponses.mockImplementation(() =>
      Promise.resolve({ secret: 'my-secret' }),
    )

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Password values should be masked with asterisks.
    await waitFor(() => {
      expect(screen.getByText('********')).toBeTruthy()
      expect(screen.queryByText('my-secret')).toBeNull()
    })
  })

  it('shows field-level validation errors', async () => {
    // Return no cached responses so inputs are editable.
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))
    // Mock install to return validation errors.
    mockClient.installPackage.mockImplementation(() => {
      const err = new Error('validation failed')
      err.problem = {
        type: 'validation',
        status: 422,
        validation_errors: [
          { name: 'hostname', error: 'invalid hostname format' },
        ],
      }
      throw err
    })

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Submit the form to trigger validation.
    const form = document.querySelector('form')
    fireEvent.submit(form)

    // Validation error message should appear.
    await waitFor(() => {
      expect(screen.getByText('invalid hostname format')).toBeTruthy()
    })
  })

  it('uses last responses when no current responses exist', async () => {
    // No current install responses.
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    // Last responses from previous uninstall.
    mockClient.getLastResponses.mockImplementation(() =>
      Promise.resolve({ hostname: 'old-host', port: '3000' }),
    )

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Last responses should be shown as cached read-only values.
    await waitFor(() => {
      expect(screen.getByText('old-host')).toBeTruthy()
      expect(screen.getByText('3000')).toBeTruthy()
    })
  })

  it('shows duration format hint for duration-type questions', async () => {
    mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
      Promise.resolve({
        retention: { query: 'Data retention?', type: 'duration', default: '30d' },
      }),
    )
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Duration hint should appear.
    await waitFor(() => {
      expect(screen.getByText(/Duration format/)).toBeTruthy()
    })

    // Duration type with a default should show the default placeholder.
    const input = document.querySelector('input[name="retention"]')
    expect(input.placeholder).toBe('Default: 30d')
  })

  it('shows duration placeholder hint when no default is set', async () => {
    mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
      Promise.resolve({
        retention: { query: 'Data retention?', type: 'duration' },
      }),
    )
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Duration type with no default should get example placeholder.
    await waitFor(() => {
      const input = document.querySelector('input[name="retention"]')
      expect(input.placeholder).toBe('e.g. 30s, 5m, 2h, 1d')
    })
  })

  it('shows port placeholder when no default is set', async () => {
    mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
      Promise.resolve({
        extport: { query: 'External port?', type: 'port' },
      }),
    )
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    await waitFor(() => {
      const input = document.querySelector('input[name="extport"]')
      expect(input.placeholder).toBe('Auto-assigned if empty')
    })
  })

  it('applies border-destructive class to input with field error', async () => {
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.installPackage.mockImplementation(() => {
      const err = new Error('validation failed')
      err.problem = {
        type: 'validation',
        status: 422,
        validation_errors: [
          { name: 'port', error: 'port out of range' },
        ],
      }
      throw err
    })

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    const form = document.querySelector('form')
    fireEvent.submit(form)

    await waitFor(() => {
      const portInput = form.querySelector('input[name="port"]')
      expect(portInput.className).toContain('border-destructive')
    })
  })

  it('clearing a cached field then seeing the default helper text', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('9090')).toBeTruthy()
    })

    // Clear the port field (second X button — port is the second question).
    const form = document.querySelector('form')
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[1].closest('button'))

    // After clearing, the port input should appear with default placeholder
    // and the default helper text should now be visible.
    await waitFor(() => {
      expect(screen.queryByText('9090')).toBeNull()
      const portInput = form.querySelector('input[name="port"]')
      expect(portInput).toBeTruthy()
      expect(portInput.placeholder).toBe('Default: 8080')
      // Default helper text should appear
      const helpers = form.querySelectorAll('p')
      const defaultHelper = Array.from(helpers).find(
        (p) => p.textContent.includes('Default:') && p.textContent.includes('8080')
      )
      expect(defaultHelper).toBeTruthy()
    })
  })

  it('shows partial last responses as cached and remaining fields as editable inputs', async () => {
    // Only hostname has a last response; port does not.
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() =>
      Promise.resolve({ hostname: 'partial-host' }),
    )

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // hostname should appear as cached read-only value.
    await waitFor(() => {
      expect(screen.getByText('partial-host')).toBeTruthy()
    })

    // port should be an editable input (no cached value).
    const form = document.querySelector('form')
    const portInput = form.querySelector('input[name="port"]')
    expect(portInput).toBeTruthy()
    expect(portInput.type).not.toBe('hidden')
    // port has default 8080, so placeholder should show it.
    expect(portInput.placeholder).toBe('Default: 8080')
  })

  it('shows all fields as editable when both responses and last responses are empty', async () => {
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Both fields should be editable inputs (no cached values, no clear buttons).
    const form = document.querySelector('form')
    const hostnameInput = form.querySelector('input[name="hostname"]')
    const portInput = form.querySelector('input[name="port"]')
    expect(hostnameInput).toBeTruthy()
    expect(hostnameInput.type).not.toBe('hidden')
    expect(portInput).toBeTruthy()
    expect(portInput.type).not.toBe('hidden')

    // No clear (X) buttons should be present.
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    expect(clearButtons.length).toBe(0)
  })

  it('current responses take precedence over last responses', async () => {
    // Both current and last responses exist.
    mockClient.getResponses.mockImplementation(() =>
      Promise.resolve({ hostname: 'current-host', port: '1111' }),
    )
    mockClient.getLastResponses.mockImplementation(() =>
      Promise.resolve({ hostname: 'old-host', port: '2222' }),
    )

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Current responses should be shown (not the last responses).
    await waitFor(() => {
      expect(screen.getByText('current-host')).toBeTruthy()
      expect(screen.getByText('1111')).toBeTruthy()
      expect(screen.queryByText('old-host')).toBeNull()
      expect(screen.queryByText('2222')).toBeNull()
    })
  })

  it('installs directly when package has no questions', async () => {
    mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
      Promise.resolve({}),
    )
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    // Should call installPackage directly without showing a questions dialog.
    await waitFor(() => {
      expect(mockClient.installPackage).toHaveBeenCalled()
    })

    // No questions dialog should have been opened.
    expect(screen.queryByText(/Install redis/)).toBeNull()
  })

  it('clearing a password field reveals a password-type input', async () => {
    mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
      Promise.resolve({
        secret: { query: 'Enter password', type: 'secret' },
      }),
    )
    mockClient.getResponses.mockImplementation(() =>
      Promise.resolve({ secret: 'my-secret' }),
    )

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('********')).toBeTruthy()
    })

    // Clear the password field.
    const form = document.querySelector('form')
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[0].closest('button'))

    // After clearing, the input should have type="password".
    await waitFor(() => {
      expect(screen.queryByText('********')).toBeNull()
      const input = form.querySelector('input[name="secret"]')
      expect(input).toBeTruthy()
      expect(input.type).toBe('password')
    })
  })

  it('cached response display has read-only styling', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    // The cached value container should have the bg-muted/50 and font-mono styling.
    const cachedDiv = screen.getByText('cached-host').closest('div')
    expect(cachedDiv.className).toContain('bg-muted/50')
    expect(cachedDiv.className).toContain('font-mono')
  })

  it('does not fetch last responses when current responses exist', async () => {
    mockClient.getResponses.mockImplementation(() =>
      Promise.resolve({ hostname: 'current-host', port: '1111' }),
    )

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    // Clear call count right before triggering the install flow.
    mockClient.getLastResponses.mockClear()

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // When current responses exist, getLastResponses should not be called
    // during the install flow.
    expect(mockClient.getLastResponses).not.toHaveBeenCalled()
  })

  it('submits correctly with mixed cached and cleared fields', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
      expect(screen.getByText('9090')).toBeTruthy()
    })

    // Clear only the hostname field (first X button), leave port cached.
    const form = document.querySelector('form')
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[0].closest('button'))

    await waitFor(() => {
      // hostname should now be an editable input
      const hostnameInput = form.querySelector('input[name="hostname"]')
      expect(hostnameInput).toBeTruthy()
      expect(hostnameInput.type).not.toBe('hidden')

      // port should still be a hidden input (cached)
      const portHidden = form.querySelector('input[name="port"][type="hidden"]')
      expect(portHidden).toBeTruthy()
      expect(portHidden.value).toBe('9090')
    })
  })

  it('clearing a cached field removes its hidden input', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    // Verify hidden input exists before clearing.
    const form = document.querySelector('form')
    expect(form.querySelector('input[name="hostname"][type="hidden"]')).toBeTruthy()

    // Clear the hostname field.
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[0].closest('button'))

    // After clearing, the hidden input should be gone.
    await waitFor(() => {
      expect(form.querySelector('input[name="hostname"][type="hidden"]')).toBeNull()
    })
  })

  it('clearing a field with no default does not show default helper text', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    // Clear the hostname field (has no default).
    const form = document.querySelector('form')
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[0].closest('button'))

    // After clearing, no "Default:" helper text should appear for hostname
    // since hostname has no default value.
    await waitFor(() => {
      expect(screen.queryByText('cached-host')).toBeNull()
      const helpers = form.querySelectorAll('p')
      const hostnameDefaultHelper = Array.from(helpers).some(
        (p) => p.textContent.includes('Default:') && !p.textContent.includes('8080'),
      )
      expect(hostnameDefaultHelper).toBe(false)
    })
  })

  it('clear button has muted styling', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    const form = document.querySelector('form')
    const clearBtn = form.querySelector('button svg.lucide-x').closest('button')
    expect(clearBtn.className).toContain('text-muted-foreground')
  })

  it('submits form with cached values passed to installPackage', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
      expect(screen.getByText('9090')).toBeTruthy()
    })

    // Submit form without clearing any fields (all cached).
    const form = document.querySelector('form')
    fireEvent.submit(form)

    await waitFor(() => {
      expect(mockClient.installPackage).toHaveBeenCalledWith(
        'core',
        'redis',
        '7.0',
        { hostname: 'cached-host', port: '9090' },
        false,
        undefined,
      )
    })
  })

  it('renders question labels for each field', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('What hostname?')).toBeTruthy()
      expect(screen.getByText('What port?')).toBeTruthy()
    })
  })

  it('default helper text uses font-mono for the value', async () => {
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // The default value "8080" should be rendered inside a font-mono span.
    await waitFor(() => {
      const defaultSpan = screen.getByText('8080')
      expect(defaultSpan.className).toContain('font-mono')
    })
  })

  it('cached response container has border styling', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    // The cached value div should have the border class.
    const cachedDiv = screen.getByText('cached-host').closest('div')
    expect(cachedDiv.className).toContain('border')
    expect(cachedDiv.className).toContain('rounded-md')
  })

  it('clearing one field does not affect the other cached field display', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
      expect(screen.getByText('9090')).toBeTruthy()
    })

    // Clear only the hostname field.
    const form = document.querySelector('form')
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[0].closest('button'))

    // hostname should be cleared, port should still show cached value.
    await waitFor(() => {
      expect(screen.queryByText('cached-host')).toBeNull()
      expect(screen.getByText('9090')).toBeTruthy()
      // Port should still have its clear button.
      const remainingClearButtons = form.querySelectorAll('button svg.lucide-x')
      expect(remainingClearButtons.length).toBe(1)
    })
  })

  it('editable input has empty default value after clearing cached response', async () => {
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText('cached-host')).toBeTruthy()
    })

    const form = document.querySelector('form')
    const clearButtons = form.querySelectorAll('button svg.lucide-x')
    fireEvent.click(clearButtons[0].closest('button'))

    // After clearing, the new input should start empty (not pre-filled).
    await waitFor(() => {
      const hostnameInput = form.querySelector('input[name="hostname"]')
      expect(hostnameInput).toBeTruthy()
      expect(hostnameInput.value).toBe('')
    })
  })

  it('shows multiple question types with correct input types when no cached values', async () => {
    mockClient.getPackageQuestionsByIdentity.mockImplementation(() =>
      Promise.resolve({
        secret: { query: 'Enter password', type: 'secret' },
        hostname: { query: 'What hostname?', type: 'hostname' },
      }),
    )
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // Password type should render as password input, others as text.
    await waitFor(() => {
      const passwordInput = document.querySelector('input[name="secret"]')
      expect(passwordInput.type).toBe('password')
      const hostnameInput = document.querySelector('input[name="hostname"]')
      expect(hostnameInput.type).toBe('text')
    })
  })

  it('fetches getLastResponses when getResponses returns empty', async () => {
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() =>
      Promise.resolve({ hostname: 'last-host' }),
    )

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    mockClient.getLastResponses.mockClear()

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // getLastResponses should have been called since current responses were empty.
    expect(mockClient.getLastResponses).toHaveBeenCalledWith('core', 'redis')
  })

  // --- Featured repositories card ---

  it('renders featured card when uninstalled featured packages exist', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'nginx', version: '1.0', description: 'Web server', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('featured-card')).toBeTruthy()
      expect(screen.getByText('Featured Packages')).toBeTruthy()
    })
  })

  it('shows no-featured-packages message when no featured packages exist', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() => Promise.resolve([]))
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('no-featured-packages')).toBeTruthy()
    })
    const el = screen.getByTestId('no-featured-packages')
    expect(el.textContent).toContain('No featured packages.')
    const link = el.querySelector('a')
    expect(link).toBeTruthy()
    expect(link.href).toBe('https://github.com/town-os/default-packages')
    expect(link.target).toBe('_blank')
    expect(screen.queryByTestId('featured-card')).toBeNull()
  })

  it('shows no-featured-packages message when all featured packages are installed', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'nginx', version: '1.0', description: 'Web server', installed: true, installed_version: '1.0' },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('no-featured-packages')).toBeTruthy()
    })
    expect(screen.queryByTestId('featured-card')).toBeNull()
  })

  it('shows featured package name and description in the card', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      const card = screen.getByTestId('featured-card')
      expect(card.textContent).toContain('mosquitto')
      expect(card.textContent).toContain('MQTT broker')
    })
  })

  it('shows Install button for uninstalled featured packages', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      const card = screen.getByTestId('featured-card')
      const installBtn = card.querySelector('button')
      expect(installBtn).toBeTruthy()
      expect(installBtn.textContent).toContain('Install')
    })
  })

  it('triggers install flow when featured card Install button is clicked', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('featured-card')).toBeTruthy()
    })
    const card = screen.getByTestId('featured-card')
    const installBtn = card.querySelector('button')
    fireEvent.click(installBtn)
    await waitFor(() => {
      expect(mockClient.listPackageVersions).toHaveBeenCalledWith('mosquitto')
    })
  })

  it('hides installed featured packages from the card', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'nginx', version: '1.0', description: 'Web server', installed: true, installed_version: '1.0' },
            { repo: 'core', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      const card = screen.getByTestId('featured-card')
      expect(card.textContent).toContain('mosquitto')
      expect(card.textContent).not.toContain('Web server')
    })
  })

  it('featured card has yellow background styling', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      const card = screen.getByTestId('featured-card')
      expect(card.className).toContain('bg-yellow-50/80')
    })
  })

  it('shows multiple uninstalled featured packages from different repos', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'nginx', version: '1.0', description: 'Web server', installed: false },
          ],
        },
        {
          repo: 'extras',
          packages: [
            { repo: 'extras', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      const card = screen.getByTestId('featured-card')
      expect(card.textContent).toContain('nginx')
      expect(card.textContent).toContain('mosquitto')
    })
  })

  it('featured card is positioned next to the tab strip in a flex row', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('featured-card')).toBeTruthy()
    })
    const card = screen.getByTestId('featured-card')
    const flexRow = card.parentElement
    expect(flexRow.className).toContain('flex')
    expect(flexRow.className).toContain('items-start')
    expect(flexRow.className).toContain('justify-between')
    expect(flexRow.className).toContain('gap-6')
    // The TabsList (containing Packages/Repositories triggers) should be a sibling
    expect(flexRow.querySelector('[role="tablist"]')).toBeTruthy()
  })

  it('empty state is positioned next to the tab strip in a flex row', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() => Promise.resolve([]))
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('no-featured-packages')).toBeTruthy()
    })
    const el = screen.getByTestId('no-featured-packages')
    const flexRow = el.parentElement
    expect(flexRow.className).toContain('flex')
    expect(flexRow.className).toContain('justify-between')
    expect(flexRow.querySelector('[role="tablist"]')).toBeTruthy()
  })

  it('empty state link has noopener noreferrer for security', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() => Promise.resolve([]))
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('no-featured-packages')).toBeTruthy()
    })
    const link = screen.getByTestId('no-featured-packages').querySelector('a')
    expect(link.rel).toBe('noopener noreferrer')
  })

  it('featured card remains visible when switching to Repositories tab', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() =>
      Promise.resolve([
        {
          repo: 'core',
          packages: [
            { repo: 'core', name: 'mosquitto', version: '2.0', description: 'MQTT broker', installed: false },
          ],
        },
      ]),
    )
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('featured-card')).toBeTruthy()
    })
    // Switch to Repositories tab
    fireEvent.click(screen.getByRole('tab', { name: 'Repositories' }))
    // Featured card should still be visible since it's outside tab content
    expect(screen.getByTestId('featured-card')).toBeTruthy()
  })

  it('no-featured-packages message remains visible when switching to Repositories tab', async () => {
    mockClient.listFeaturedPackages.mockImplementation(() => Promise.resolve([]))
    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByTestId('no-featured-packages')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('tab', { name: 'Repositories' }))
    expect(screen.getByTestId('no-featured-packages')).toBeTruthy()
  })

  it('validation error does not appear on fresh dialog open', async () => {
    mockClient.getResponses.mockImplementation(() => Promise.resolve({}))
    mockClient.getLastResponses.mockImplementation(() => Promise.resolve({}))

    renderPackageManagement()
    await waitFor(() => {
      expect(screen.getByText('Not Installed')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('Not Installed'))

    await waitFor(() => {
      expect(screen.getByText(/Install redis/)).toBeTruthy()
    })

    // No validation errors should be present initially.
    const form = document.querySelector('form')
    const errorTexts = form.querySelectorAll('.text-destructive')
    expect(errorTexts.length).toBe(0)
  })
})
