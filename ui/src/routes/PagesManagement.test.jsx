import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockListPages = vi.fn(() =>
  Promise.resolve({
    entries: [
      {
        name: 'my-site',
        repo_url: 'https://github.com/user/repo.git',
        branch: 'main',
        domain: 'my-site',
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
    repo_url: 'https://github.com/user/new.git',
    branch: 'main',
    domain: 'new-site',
    status: 'pending',
  }),
)
const mockUpdatePage = vi.fn(() =>
  Promise.resolve({
    name: 'my-site',
    repo_url: 'https://github.com/user/updated.git',
    branch: 'main',
    domain: 'my-site',
    status: 'active',
  }),
)
const mockRemovePage = vi.fn(() => Promise.resolve())
const mockRebuildPage = vi.fn(() =>
  Promise.resolve({
    name: 'my-site',
    status: 'active',
  }),
)

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listPages: mockListPages,
    createPage: mockCreatePage,
    updatePage: mockUpdatePage,
    removePage: mockRemovePage,
    rebuildPage: mockRebuildPage,
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

  it('displays the repository URL', async () => {
    renderPages()
    await waitFor(() => {
      expect(screen.getByText('https://github.com/user/repo.git')).toBeTruthy()
    })
  })

  it('displays the branch badge', async () => {
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

  it('displays pending status with secondary badge', async () => {
    mockListPages.mockResolvedValueOnce({
      entries: [
        {
          name: 'pending-site',
          repo_url: 'https://example.com/repo.git',
          branch: 'main',
          domain: 'pending-site',
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
      expect(screen.getByText('pending')).toBeTruthy()
    })
  })

  it('sets document title', async () => {
    renderPages()
    await waitFor(() => {
      expect(document.title).toBe('Town OS - Pages')
    })
  })
})
