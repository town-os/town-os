import { describe, it, expect, vi, beforeEach, beforeAll } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockListPages = vi.fn(() =>
  Promise.resolve({
    entries: [
      {
        name: 'my-site',
        repo_url: 'https://github.com/user/repo.git',
        branch: 'main',
        domain: 'my-site',
        source_type: 'git',
        image: '',
        image_directory: '',
        status: 'active',
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ],
    has_more: false,
    total_pages: 1,
    total_count: 1,
  }),
)
const mockCreatePage = vi.fn(() =>
  Promise.resolve({
    name: 'new-site',
    repo_url: '',
    branch: '',
    domain: 'new-site',
    source_type: 'archive',
    image: '',
    image_directory: '',
    status: 'pending',
  }),
)
const mockUpdatePage = vi.fn(() =>
  Promise.resolve({
    name: 'my-site',
    repo_url: 'https://github.com/user/updated.git',
    branch: 'main',
    domain: 'my-site',
    source_type: 'git',
    image: '',
    image_directory: '',
    status: 'active',
  }),
)
const mockRemovePage = vi.fn(() => Promise.resolve())
const mockRebuildPage = vi.fn(() =>
  Promise.resolve({
    name: 'my-site',
    source_type: 'git',
    status: 'active',
  }),
)
const mockUploadPageArchive = vi.fn(() =>
  Promise.resolve({
    name: 'my-site',
    source_type: 'archive',
    status: 'active',
  }),
)

const mockListNetworks = vi.fn(() =>
  Promise.resolve([
    { name: 'home', tld: 'home', enabled: true },
    { name: 'fart', tld: 'fart', enabled: true },
    { name: 'off', tld: 'off', enabled: false },
  ]),
)

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listPages: mockListPages,
    createPage: mockCreatePage,
    updatePage: mockUpdatePage,
    removePage: mockRemovePage,
    rebuildPage: mockRebuildPage,
    uploadPageArchive: mockUploadPageArchive,
    listNetworks: mockListNetworks,
  }),
}))

import PagesManagement from './PagesManagement.jsx'

function renderPages() {
  return render(
    <MemoryRouter>
      <PagesManagement />
    </MemoryRouter>,
  )
}

describe('PagesManagement component', () => {
  beforeEach(() => {
    mockListPages.mockClear()
    mockCreatePage.mockClear()
    mockUpdatePage.mockClear()
    mockRemovePage.mockClear()
    mockRebuildPage.mockClear()
    mockUploadPageArchive.mockClear()
  })

  it('renders the Pages heading', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Pages')).toBeTruthy()
    })
  })

  it('renders the subheading', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Manage static HTML content sites')).toBeTruthy()
    })
  })

  it('renders the Create Page button', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
  })

  it('calls listPages on mount', async () => {
    renderPages()
    await waitFor(() => {
      expect(mockListPages).toHaveBeenCalled()
    })
  })

  it('displays a page entry from the list', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getAllByText('my-site').length).toBeGreaterThan(0)
    })
  })

  it('displays the repository URL for git pages', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('https://github.com/user/repo.git')).toBeTruthy()
    })
  })

  it('displays the branch badge for git pages', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('main')).toBeTruthy()
    })
  })

  it('displays active status badge', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('active')).toBeTruthy()
    })
  })

  it('displays source type badge', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Git Repository')).toBeTruthy()
    })
  })

  it('renders Edit button for each row', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Edit')).toBeTruthy()
    })
  })

  it('renders Delete button for each row', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Delete')).toBeTruthy()
    })
  })

  it('opens create dialog when Create Page button is clicked', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeTruthy()
    })
  })

  it('populates the network selector from listNetworks with enabled networks only', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Page'))

    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'fart' })).toBeTruthy()
    })
    expect(screen.getByRole('option', { name: 'home' })).toBeTruthy()
    // Disabled networks are not offerable targets.
    expect(screen.queryByRole('option', { name: 'off' })).toBeNull()
  })

  it('submits the selected network when creating a page', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeTruthy()
    })

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'secret' } })
    // Move the page onto the fart network.
    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'fart' })).toBeTruthy()
    })
    fireEvent.change(document.getElementById('create-network'), { target: { value: 'fart' } })

    fireEvent.submit(document.getElementById('create-name').closest('form'))

    await waitFor(() => {
      expect(mockCreatePage).toHaveBeenCalled()
    })
    // network is the 8th positional arg of createPage.
    expect(mockCreatePage.mock.calls[0][7]).toBe('fart')
  })

  it('defaults a page to the home network when nothing is chosen', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeTruthy()
    })

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'blog' } })
    fireEvent.submit(document.getElementById('create-name').closest('form'))

    await waitFor(() => {
      expect(mockCreatePage).toHaveBeenCalled()
    })
    expect(mockCreatePage.mock.calls[0][7]).toBe('home')
  })

  it('opens edit dialog when Edit button is clicked', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Edit')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Edit'))
    await waitFor(() => {
      expect(screen.getByText('Edit Page: my-site')).toBeTruthy()
    })
  })

  it('opens delete confirmation when Delete button is clicked', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Delete')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Delete'))
    await waitFor(() => {
      expect(screen.getByText('Delete Page')).toBeTruthy()
    })
  })

  it('shows loading state when no data', async () => {
    mockListPages.mockReturnValueOnce(new Promise(() => {}))
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Loading...')).toBeTruthy()
    })
  })

  it('shows empty state when no pages exist', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('No data')).toBeTruthy()
    })
  })

  it('displays error status with destructive badge', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'err-site',
          repo_url: 'https://example.com/repo.git',
          branch: 'main',
          domain: 'err-site',
          source_type: 'git',
          image: '',
          image_directory: '',
          status: 'error',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('error')).toBeTruthy()
    })
  })

  it('displays pending status with provisioning spinner', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'pending-site',
          repo_url: '',
          branch: '',
          domain: 'pending-site',
          source_type: 'archive',
          image: '',
          image_directory: '',
          status: 'pending',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Provisioning')).toBeTruthy()
    })
    // Verify the spinner icon is present (Loader2 renders as an SVG with animate-spin)
    const badge = screen.getByText('Provisioning').closest('[class*="badge"]') || screen.getByText('Provisioning').parentElement
    const spinner = badge.querySelector('.animate-spin')
    expect(spinner).toBeTruthy()
  })

  it('displays archive source type badge', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'archive-site',
          repo_url: '',
          branch: '',
          domain: 'archive-site',
          source_type: 'archive',
          image: '',
          image_directory: '',
          status: 'pending',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Archive Upload')).toBeTruthy()
    })
  })

  it('displays container image source type badge', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'image-site',
          repo_url: '',
          branch: '',
          domain: 'image-site',
          source_type: 'container_image',
          image: 'nginx:latest',
          image_directory: '/usr/share/nginx/html',
          status: 'active',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Container Image')).toBeTruthy()
    })
  })

  it('displays container image in repository column for container_image pages', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'image-site',
          repo_url: '',
          branch: '',
          domain: 'image-site',
          source_type: 'container_image',
          image: 'nginx:latest',
          image_directory: '/usr/share/nginx/html',
          status: 'active',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('nginx:latest')).toBeTruthy()
    })
  })

  it('shows upload button for archive pages', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'archive-site',
          repo_url: '',
          branch: '',
          domain: 'archive-site',
          source_type: 'archive',
          image: '',
          image_directory: '',
          status: 'pending',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByTitle('Upload archive')).toBeTruthy()
    })
  })

  it('shows rebuild button for git pages', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByTitle('Rebuild from git')).toBeTruthy()
    })
  })

  it('sets document title', async () => {
    renderPages()
    await waitFor(() => {
      expect(document.title).toBe('Town OS - Pages')
    })
  })

  it('opens upload dialog when upload button is clicked for archive pages', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'archive-site',
          repo_url: '',
          branch: '',
          domain: 'archive-site',
          source_type: 'archive',
          image: '',
          image_directory: '',
          status: 'pending',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByTitle('Upload archive')).toBeTruthy()
    })
    fireEvent.click(screen.getByTitle('Upload archive'))
    await waitFor(() => {
      expect(screen.getByText('Upload Archive: archive-site')).toBeTruthy()
    })
  })

  it('shows archive file input in create dialog when archive source type selected', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => {
      // Archive is the default source type, so the file input should be visible.
      expect(screen.getByLabelText('Archive File')).toBeTruthy()
    })
  })

  it('create dialog has source type selector', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => {
      expect(screen.getByLabelText('Source Type')).toBeTruthy()
      // Default source type is archive so archive file input should be visible.
      expect(screen.getByLabelText('Archive File')).toBeTruthy()
    })
  })

  it('create dialog does not show git fields for default archive source type', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Create Page')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => {
      // Archive is default, so git/container fields should not be present.
      expect(screen.queryByLabelText('Repository URL')).toBeNull()
      expect(screen.queryByLabelText('Branch')).toBeNull()
      expect(screen.queryByLabelText('Container Image')).toBeNull()
      expect(screen.queryByLabelText('Image Directory')).toBeNull()
    })
  })

  it('shows rebuild button for container_image pages', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'image-site',
          repo_url: '',
          branch: '',
          domain: 'image-site',
          source_type: 'container_image',
          image: 'nginx:latest',
          image_directory: '/usr/share/nginx/html',
          status: 'active',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByTitle('Re-extract from image')).toBeTruthy()
    })
  })

  it('shows dash for branch column on archive pages', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'archive-site',
          repo_url: '',
          branch: '',
          domain: 'archive-site',
          source_type: 'archive',
          image: '',
          image_directory: '',
          status: 'pending',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      // The branch column shows "-" for archive pages.
      const dashes = screen.getAllByText('-')
      expect(dashes.length).toBeGreaterThan(0)
    })
  })

  it('shows container image edit fields in edit dialog for container_image pages', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'image-site',
          repo_url: '',
          branch: '',
          domain: 'image-site',
          source_type: 'container_image',
          image: 'nginx:latest',
          image_directory: '/usr/share/nginx/html',
          status: 'active',
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      ],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('Edit')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Edit'))
    await waitFor(() => {
      expect(screen.getByText('Edit Page: image-site')).toBeTruthy()
      expect(screen.getByDisplayValue('nginx:latest')).toBeTruthy()
      expect(screen.getByDisplayValue('/usr/share/nginx/html')).toBeTruthy()
    })
  })
})

describe('PagesManagement provisioning behavior', () => {
  beforeAll(() => {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    // Radix UI pointer capture methods not available in jsdom
    HTMLElement.prototype.hasPointerCapture = () => false
    HTMLElement.prototype.setPointerCapture = () => {}
    HTMLElement.prototype.releasePointerCapture = () => {}
    HTMLElement.prototype.scrollIntoView = () => {}
  })

  beforeEach(() => {
    mockListPages.mockClear()
    mockCreatePage.mockClear()
    mockUpdatePage.mockClear()
    mockRemovePage.mockClear()
    mockRebuildPage.mockClear()
    mockUploadPageArchive.mockClear()
  })

  function setFileOnInput(input, file) {
    Object.defineProperty(input, 'files', {
      value: [file],
      configurable: true,
    })
  }

  async function selectSourceType(label) {
    // The create dialog has two comboboxes now — the shadcn source-type trigger
    // and the native network <select> — so target the source-type one by id
    // rather than assuming it is the only combobox in the dialog.
    const trigger = document.getElementById('create-source-type')
    fireEvent.pointerDown(trigger, { button: 0, pointerType: 'mouse' })
    await waitFor(() => {
      expect(screen.getByRole('option', { name: label })).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('option', { name: label }))
  }

  it('archive page without file closes immediately', async () => {
    mockListPages.mockResolvedValue({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    mockCreatePage.mockResolvedValue({
      name: 'my-archive',
      source_type: 'archive',
      status: 'pending',
    })

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => expect(screen.getByLabelText('Name')).toBeTruthy())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'my-archive' } })
    fireEvent.click(screen.getByText('Create'))

    await waitFor(() => {
      expect(mockCreatePage).toHaveBeenCalled()
    })
    // Spinner should not appear for archive without file
    await waitFor(() => {
      expect(screen.queryByText('Provisioning...')).toBeNull()
    })
  })

  it('archive page with file shows provisioning spinner during upload', async () => {
    mockListPages.mockResolvedValue({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    mockCreatePage.mockResolvedValue({
      name: 'my-archive',
      source_type: 'archive',
      status: 'pending',
    })

    let resolveUpload
    mockUploadPageArchive.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveUpload = resolve
      }),
    )

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => expect(screen.getByLabelText('Name')).toBeTruthy())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'my-archive' } })

    const file = new File(['content'], 'site.tar.gz', { type: 'application/gzip' })
    setFileOnInput(screen.getByLabelText('Archive File'), file)

    fireEvent.click(screen.getByText('Create'))

    await waitFor(() => {
      expect(screen.getByText('Provisioning...')).toBeTruthy()
    })

    resolveUpload({ name: 'my-archive', source_type: 'archive', status: 'active' })

    await waitFor(() => {
      expect(screen.queryByText('Provisioning...')).toBeNull()
    })
  })

  it('form inputs disabled during provisioning', async () => {
    mockListPages.mockResolvedValue({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    mockCreatePage.mockResolvedValue({
      name: 'my-archive',
      source_type: 'archive',
      status: 'pending',
    })
    mockUploadPageArchive.mockReturnValueOnce(new Promise(() => {}))

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => expect(screen.getByLabelText('Name')).toBeTruthy())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'my-archive' } })

    const file = new File(['content'], 'site.tar.gz', { type: 'application/gzip' })
    setFileOnInput(screen.getByLabelText('Archive File'), file)

    fireEvent.click(screen.getByText('Create'))

    await waitFor(() => {
      expect(screen.getByText('Provisioning...')).toBeTruthy()
    })

    const fieldset = screen.getByLabelText('Name').closest('fieldset')
    expect(fieldset.disabled).toBe(true)
  })

  it('dialog cannot be closed during provisioning', async () => {
    mockListPages.mockResolvedValue({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    mockCreatePage.mockResolvedValue({
      name: 'my-archive',
      source_type: 'archive',
      status: 'pending',
    })
    mockUploadPageArchive.mockReturnValueOnce(new Promise(() => {}))

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => expect(screen.getByLabelText('Name')).toBeTruthy())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'my-archive' } })

    const file = new File(['content'], 'site.tar.gz', { type: 'application/gzip' })
    setFileOnInput(screen.getByLabelText('Archive File'), file)

    fireEvent.click(screen.getByText('Create'))

    await waitFor(() => {
      expect(screen.getByText('Provisioning...')).toBeTruthy()
    })

    fireEvent.keyDown(document, { key: 'Escape' })

    // Dialog should still be open
    expect(screen.getByText('Provisioning...')).toBeTruthy()
  })

  it('git page shows spinner and closes on active status after polling', async () => {
    mockCreatePage.mockResolvedValueOnce({
      name: 'git-site',
      source_type: 'git',
      status: 'pending',
    })
    // Initial load
    mockListPages.mockResolvedValueOnce({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    // First poll returns active
    mockListPages.mockResolvedValueOnce({
      entries: [{ name: 'git-site', source_type: 'git', status: 'active' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    // Subsequent calls
    mockListPages.mockResolvedValue({
      entries: [{ name: 'git-site', source_type: 'git', status: 'active' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await selectSourceType('Git Repository')

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'git-site' } })
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/user/repo.git' },
    })

    // Install fake timers just before submit so polling setTimeout is controlled
    vi.useFakeTimers()

    fireEvent.click(screen.getByText('Create'))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(screen.getByText('Provisioning...')).toBeTruthy()

    // Advance past the 2s poll delay
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(screen.queryByText('Provisioning...')).toBeNull()

    vi.useRealTimers()
  })

  it('git page shows error toast when polling finds error status', async () => {
    mockCreatePage.mockResolvedValueOnce({
      name: 'git-site',
      source_type: 'git',
      status: 'pending',
    })
    mockListPages.mockResolvedValueOnce({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    // Poll returns error status
    mockListPages.mockResolvedValueOnce({
      entries: [{ name: 'git-site', source_type: 'git', status: 'error' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    mockListPages.mockResolvedValue({
      entries: [{ name: 'git-site', source_type: 'git', status: 'error' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await selectSourceType('Git Repository')

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'git-site' } })
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/user/repo.git' },
    })

    vi.useFakeTimers()

    fireEvent.click(screen.getByText('Create'))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(screen.getByText('Provisioning...')).toBeTruthy()

    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(screen.queryByText('Provisioning...')).toBeNull()

    vi.useRealTimers()
  })

  it('git page creation error resets provisioning state', async () => {
    mockCreatePage.mockRejectedValueOnce(new Error('server error'))
    mockListPages.mockResolvedValue({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await selectSourceType('Git Repository')

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'fail-site' } })
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/user/repo.git' },
    })

    fireEvent.click(screen.getByText('Create'))

    // After the error, dialog should remain open and provisioning should be reset
    await waitFor(() => {
      expect(screen.queryByText('Provisioning...')).toBeNull()
    })
    // Form should be re-enabled after error
    const fieldset = screen.getByLabelText('Name').closest('fieldset')
    expect(fieldset.disabled).toBe(false)
  })

  it('cancel button disabled during provisioning', async () => {
    mockListPages.mockResolvedValue({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    mockCreatePage.mockResolvedValue({
      name: 'my-archive',
      source_type: 'archive',
      status: 'pending',
    })
    mockUploadPageArchive.mockReturnValueOnce(new Promise(() => {}))

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await waitFor(() => expect(screen.getByLabelText('Name')).toBeTruthy())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'my-archive' } })

    const file = new File(['content'], 'site.tar.gz', { type: 'application/gzip' })
    setFileOnInput(screen.getByLabelText('Archive File'), file)

    fireEvent.click(screen.getByText('Create'))

    await waitFor(() => {
      expect(screen.getByText('Provisioning...')).toBeTruthy()
    })

    const cancelBtn = screen.getByText('Cancel')
    expect(cancelBtn.disabled).toBe(true)
  })

  it('container image page shows error toast on poll failure', async () => {
    mockCreatePage.mockResolvedValueOnce({
      name: 'image-site',
      source_type: 'container_image',
      status: 'pending',
    })
    mockListPages.mockResolvedValueOnce({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    // Poll returns error status
    mockListPages.mockResolvedValueOnce({
      entries: [{ name: 'image-site', source_type: 'container_image', status: 'error' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    mockListPages.mockResolvedValue({
      entries: [{ name: 'image-site', source_type: 'container_image', status: 'error' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await selectSourceType('Container Image')

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'image-site' } })
    fireEvent.change(screen.getByLabelText('Container Image'), {
      target: { value: 'nginx:latest' },
    })
    fireEvent.change(screen.getByLabelText('Image Directory'), {
      target: { value: '/usr/share/nginx/html' },
    })

    vi.useFakeTimers()

    fireEvent.click(screen.getByText('Create'))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(screen.getByText('Provisioning...')).toBeTruthy()

    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    // Dialog should close after error detected
    expect(screen.queryByText('Provisioning...')).toBeNull()

    vi.useRealTimers()
  })

  it('container image page polls until active', async () => {
    mockCreatePage.mockResolvedValueOnce({
      name: 'image-site',
      source_type: 'container_image',
      status: 'pending',
    })
    mockListPages.mockResolvedValueOnce({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    mockListPages.mockResolvedValueOnce({
      entries: [{ name: 'image-site', source_type: 'container_image', status: 'active' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })
    mockListPages.mockResolvedValue({
      entries: [{ name: 'image-site', source_type: 'container_image', status: 'active' }],
      has_more: false,
      total_pages: 1,
      total_count: 1,
    })

    renderPages()
    await waitFor(() => expect(screen.getByText('Create Page')).toBeTruthy())

    fireEvent.click(screen.getByText('Create Page'))
    await selectSourceType('Container Image')

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'image-site' } })
    fireEvent.change(screen.getByLabelText('Container Image'), {
      target: { value: 'nginx:latest' },
    })
    fireEvent.change(screen.getByLabelText('Image Directory'), {
      target: { value: '/usr/share/nginx/html' },
    })

    vi.useFakeTimers()

    fireEvent.click(screen.getByText('Create'))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(screen.getByText('Provisioning...')).toBeTruthy()

    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(screen.queryByText('Provisioning...')).toBeNull()

    vi.useRealTimers()
  })
})
