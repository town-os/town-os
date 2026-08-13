import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// --- Extract pure functions for direct testing ---
// We re-implement the logic inline since the module doesn't export them,
// then verify the component behavior matches.

const UNITS = {
  B: 1,
  MB: 1024 * 1024,
  GB: 1024 * 1024 * 1024,
  TB: 1024 * 1024 * 1024 * 1024,
}

function formatQuotaText(bytes) {
  if (!bytes || bytes === 0) return 'none'
  if (bytes >= UNITS.TB) return `${(bytes / UNITS.TB).toFixed(2)} TB`
  if (bytes >= UNITS.GB) return `${(bytes / UNITS.GB).toFixed(2)} GB`
  if (bytes >= UNITS.MB) return `${(bytes / UNITS.MB).toFixed(2)} MB`
  return `${bytes} B`
}

function decomposeQuota(bytes) {
  if (!bytes || bytes === 0) return ['', 'GB']
  if (bytes >= UNITS.TB && bytes % UNITS.TB === 0) return [bytes / UNITS.TB, 'TB']
  if (bytes >= UNITS.GB && bytes % UNITS.GB === 0) return [bytes / UNITS.GB, 'GB']
  if (bytes >= UNITS.MB && bytes % UNITS.MB === 0) return [bytes / UNITS.MB, 'MB']
  return [bytes, 'B']
}

function deriveServiceName(volumeName) {
  const parts = volumeName.split('/')
  if (parts.length < 3) return ''
  return `town-os-package--${parts[0]}-${parts[1]}-${parts[2]}.service`
}

// --- Pure function unit tests ---

describe('formatQuotaText', () => {
  it('returns "none" for 0', () => {
    expect(formatQuotaText(0)).toBe('none')
  })

  it('returns "none" for null/undefined', () => {
    expect(formatQuotaText(null)).toBe('none')
    expect(formatQuotaText(undefined)).toBe('none')
  })

  it('formats bytes', () => {
    expect(formatQuotaText(512)).toBe('512 B')
  })

  it('formats megabytes', () => {
    expect(formatQuotaText(10 * UNITS.MB)).toBe('10.00 MB')
  })

  it('formats gigabytes', () => {
    expect(formatQuotaText(5 * UNITS.GB)).toBe('5.00 GB')
  })

  it('formats terabytes', () => {
    expect(formatQuotaText(2 * UNITS.TB)).toBe('2.00 TB')
  })

  it('formats fractional gigabytes', () => {
    expect(formatQuotaText(1.5 * UNITS.GB)).toBe('1.50 GB')
  })

  it('formats fractional terabytes as GB when below 1 TB', () => {
    // 0.5 TB < UNITS.TB, so it falls through to the GB branch
    expect(formatQuotaText(0.5 * UNITS.TB)).toBe('512.00 GB')
  })

  it('uses MB when between MB and GB', () => {
    expect(formatQuotaText(500 * UNITS.MB)).toBe('500.00 MB')
  })
})

describe('decomposeQuota', () => {
  it('returns empty value and GB unit for 0', () => {
    expect(decomposeQuota(0)).toEqual(['', 'GB'])
  })

  it('returns empty value and GB unit for null', () => {
    expect(decomposeQuota(null)).toEqual(['', 'GB'])
  })

  it('returns empty value and GB unit for undefined', () => {
    expect(decomposeQuota(undefined)).toEqual(['', 'GB'])
  })

  it('decomposes exact TB', () => {
    expect(decomposeQuota(2 * UNITS.TB)).toEqual([2, 'TB'])
  })

  it('decomposes exact GB', () => {
    expect(decomposeQuota(5 * UNITS.GB)).toEqual([5, 'GB'])
  })

  it('decomposes exact MB', () => {
    expect(decomposeQuota(100 * UNITS.MB)).toEqual([100, 'MB'])
  })

  it('falls back to bytes for non-aligned values', () => {
    expect(decomposeQuota(12345)).toEqual([12345, 'B'])
  })

  it('decomposes 1 TB', () => {
    expect(decomposeQuota(UNITS.TB)).toEqual([1, 'TB'])
  })

  it('decomposes 1 GB', () => {
    expect(decomposeQuota(UNITS.GB)).toEqual([1, 'GB'])
  })

  it('decomposes 1 MB', () => {
    expect(decomposeQuota(UNITS.MB)).toEqual([1, 'MB'])
  })

  it('uses GB not TB for non-aligned large values', () => {
    // 1.5 TB is not evenly divisible by TB but is by GB
    const val = 1536 * UNITS.GB
    expect(decomposeQuota(val)).toEqual([1536, 'GB'])
  })

  it('uses MB for values not aligned to GB', () => {
    // 1.5 GB is not evenly divisible by GB but is by MB
    const val = 1536 * UNITS.MB
    expect(decomposeQuota(val)).toEqual([1536, 'MB'])
  })
})

describe('deriveServiceName', () => {
  it('derives service name from 4-part volume name', () => {
    expect(deriveServiceName('repo/name/1.0/data')).toBe('town-os-package--repo-name-1.0.service')
  })

  it('derives service name from 3-part volume name', () => {
    expect(deriveServiceName('core/nginx/2.0')).toBe('town-os-package--core-nginx-2.0.service')
  })

  it('returns empty string for 2-part name', () => {
    expect(deriveServiceName('core/nginx')).toBe('')
  })

  it('returns empty string for single name', () => {
    expect(deriveServiceName('single')).toBe('')
  })

  it('returns empty string for empty string', () => {
    expect(deriveServiceName('')).toBe('')
  })

  it('uses only first 3 parts for longer paths', () => {
    expect(deriveServiceName('repo/pkg/1.0/vol/sub')).toBe('town-os-package--repo-pkg-1.0.service')
  })
})

// --- Component rendering tests ---

const mockListFilesystems = vi.fn(() =>
  Promise.resolve({
    entries: [
      { name: 'mydata', quota: 1073741824, state: 'user' },
    ],
    has_more: false,
    total_pages: 1,
    total_count: 1,
  }),
)

const mockListPackageVolumes = vi.fn(() => Promise.resolve([]))
const mockRemovePackageVolume = vi.fn(() => Promise.resolve())
const mockRemovePackageVolumeGroup = vi.fn(() => Promise.resolve())
const mockCreateFilesystem = vi.fn(() => Promise.resolve())
const mockModifyFilesystem = vi.fn(() => Promise.resolve())
const mockRemoveFilesystem = vi.fn(() => Promise.resolve())
// The swap panel polls ping. Default to a pool that cannot swap, because that
// is what a multi-disk Town OS looks like and it is the case the panel exists
// to explain; individual tests override it.
const mockPing = vi.fn(() =>
  Promise.resolve({
    swap: {
      supported: false,
      reason: 'multi_device',
      devices: 4,
      data_profiles: ['RAID5'],
      active: false,
    },
  }),
)

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listFilesystems: mockListFilesystems,
    listPackageVolumes: mockListPackageVolumes,
    removePackageVolume: mockRemovePackageVolume,
    removePackageVolumeGroup: mockRemovePackageVolumeGroup,
    createFilesystem: mockCreateFilesystem,
    modifyFilesystem: mockModifyFilesystem,
    removeFilesystem: mockRemoveFilesystem,
    downloadArchive: vi.fn(() => Promise.resolve({ body: null, blob: () => Promise.resolve(new Blob()) })),
    uploadArchive: vi.fn(() => Promise.resolve({ message: 'ok' })),
    getSetting: vi.fn(() => Promise.resolve('53687091200')),
    ping: mockPing,
  }),
}))

import StorageManagement from './StorageManagement.jsx'

function renderStorageManagement() {
  return render(
    <MemoryRouter>
      <StorageManagement />
    </MemoryRouter>,
  )
}

describe('StorageManagement component', () => {
  beforeEach(() => {
    mockListFilesystems.mockClear()
    mockListPackageVolumes.mockClear()
    mockRemovePackageVolume.mockClear()
    mockRemovePackageVolumeGroup.mockClear()
    mockCreateFilesystem.mockClear()
    mockModifyFilesystem.mockClear()
    mockRemoveFilesystem.mockClear()
    // Reset to defaults
    mockListFilesystems.mockImplementation(() =>
      Promise.resolve({
        entries: [{ name: 'mydata', quota: 1073741824, state: 'user' }],
        has_more: false,
        total_pages: 1,
        total_count: 1,
      }),
    )
    mockListPackageVolumes.mockImplementation(() => Promise.resolve([]))
    mockPing.mockImplementation(() =>
      Promise.resolve({
        swap: {
          supported: false,
          reason: 'multi_device',
          devices: 4,
          data_profiles: ['RAID5'],
          active: false,
        },
      }),
    )
  })

  it('renders the Storage heading', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Storage')).toBeTruthy()
    })
  })

  it('renders the subheading', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Manage btrfs subvolumes')).toBeTruthy()
    })
  })

  it('renders Create Filesystem button', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Create Filesystem')).toBeTruthy()
    })
  })

  it('renders Show uninstalled volumes checkbox', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Show uninstalled volumes')).toBeTruthy()
    })
  })

  it('renders user filesystem data after loading', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getAllByText('mydata').length).toBeGreaterThanOrEqual(1)
    })
  })

  it('renders User Filesystems section heading', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('User Filesystems')).toBeTruthy()
    })
  })

  it('calls listFilesystems on mount', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(mockListFilesystems).toHaveBeenCalled()
    })
  })

  it('calls listPackageVolumes on mount', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(mockListPackageVolumes).toHaveBeenCalled()
    })
  })

  it('renders Modify button for user filesystems', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getAllByText('mydata').length).toBeGreaterThanOrEqual(1)
    })
    expect(screen.getAllByText('Modify').length).toBeGreaterThanOrEqual(1)
  })

  it('opens create dialog when Create Filesystem is clicked', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Create Filesystem')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Filesystem'))
    await waitFor(() => {
      expect(screen.getByText('Create')).toBeTruthy()
    })
  })

  it('renders Package Volumes section when package groups exist', async () => {
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
  })

  it('clicking delete icon and confirming calls removeFilesystem', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getAllByText('mydata').length).toBeGreaterThanOrEqual(1)
    })
    // Click the destructive-styled delete button for the "mydata" row
    const deleteButtons = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    expect(deleteButtons.length).toBeGreaterThanOrEqual(1)
    fireEvent.click(deleteButtons[0])
    // Confirm dialog should appear
    await waitFor(() => {
      expect(screen.getByText('Delete Filesystem')).toBeTruthy()
    })
    // Click the destructive "Delete" confirm button
    const confirmButton = screen.getByRole('button', { name: 'Delete' })
    fireEvent.click(confirmButton)
    await waitFor(() => {
      expect(mockRemoveFilesystem).toHaveBeenCalledWith('mydata')
    })
  })

  it('clicking Modify opens the modify dialog with Save Changes button', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getAllByText('mydata').length).toBeGreaterThanOrEqual(1)
    })
    // Click the Modify button (not the column header) on the user filesystem row
    const modifyButtons = screen.getAllByRole('button', { name: /Modify/ })
    expect(modifyButtons.length).toBeGreaterThanOrEqual(1)
    fireEvent.click(modifyButtons[0])
    await waitFor(() => {
      expect(screen.getByText('Save Changes')).toBeTruthy()
    })
  })

  it('user filesystem modify dialog shows Name field for renaming', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getAllByText('mydata').length).toBeGreaterThanOrEqual(1)
    })
    const modifyButtons = screen.getAllByRole('button', { name: /Modify/ })
    fireEvent.click(modifyButtons[0])
    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeTruthy()
    })
  })

  it('package volume modify dialog does not show Name field', async () => {
    // No user filesystems for this test to avoid ambiguity
    mockListFilesystems.mockResolvedValue({
      entries: [],
      has_more: false,
      total_pages: 1,
      total_count: 0,
    })
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        effective_name: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    // Expand the package tree to see version rows
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('nginx'))
    // Version row appears; expand it to see leaves.
    await waitFor(() => {
      expect(screen.getByText('Version 1.0')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Version 1.0'))
    // Now click the Modify button on the leaf row
    await waitFor(() => {
      const modifyButtons = screen.getAllByRole('button', { name: /Modify/ })
      expect(modifyButtons.length).toBeGreaterThanOrEqual(1)
    })
    const modifyButtons = screen.getAllByRole('button', { name: /Modify/ })
    fireEvent.click(modifyButtons[modifyButtons.length - 1])
    await waitFor(() => {
      expect(screen.getByText('Save Changes')).toBeTruthy()
    })
    // The modify dialog should have quota but NOT a Name field
    expect(screen.getByLabelText('Quota (0 = unlimited)')).toBeTruthy()
    expect(screen.queryByLabelText('Name')).toBeNull()
  })

  it('create form submits and calls createFilesystem', async () => {
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Create Filesystem')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Create Filesystem'))
    await waitFor(() => {
      expect(screen.getByText('Create')).toBeTruthy()
    })
    // Fill in the name field
    const nameInput = screen.getByLabelText('Name')
    fireEvent.change(nameInput, { target: { value: 'newfs' } })
    // Fill in the quota field
    const quotaInput = screen.getByLabelText('Quota (0 = unlimited)')
    fireEvent.change(quotaInput, { target: { value: '2' } })
    // Submit the form by clicking the Create button
    const createButton = screen.getByRole('button', { name: 'Create' })
    fireEvent.click(createButton)
    await waitFor(() => {
      expect(mockCreateFilesystem).toHaveBeenCalledWith({
        name: 'newfs',
        quota: 2 * 1024 * 1024 * 1024,
      })
    })
  })

  it('renders delete button at every level of the package volumes tree', async () => {
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        effective_name: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    // Package row is visible from the start; it must already carry a
    // destructive delete button even before expansion (so the top-level
    // cascade is reachable without drilling in).
    const topLevelDestructive = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    // user filesystem (1) + package-level (1) = at least 2
    expect(topLevelDestructive.length).toBeGreaterThanOrEqual(2)

    // Expand the package → version row appears with its own delete button
    fireEvent.click(screen.getByText('nginx'))
    await waitFor(() => {
      expect(screen.getByText('Version 1.0')).toBeTruthy()
    })
    const afterPkgExpand = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    // user + package + version = at least 3
    expect(afterPkgExpand.length).toBeGreaterThanOrEqual(3)

    // Expand the version → leaf row appears with its own delete button
    fireEvent.click(screen.getByText('Version 1.0'))
    await waitFor(() => {
      expect(screen.getByText('data')).toBeTruthy()
    })
    const afterVerExpand = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    // user + package + version + leaf = at least 4
    expect(afterVerExpand.length).toBeGreaterThanOrEqual(4)
  })

  it('non-leaf package rows do not render Modify / Upload / Download actions', async () => {
    mockListFilesystems.mockResolvedValue({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        effective_name: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    // Only the package row is visible — no Modify/Upload/Download should exist yet.
    expect(screen.queryByRole('button', { name: /Modify/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Download archive' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Upload archive' })).toBeNull()

    fireEvent.click(screen.getByText('nginx'))
    await waitFor(() => {
      expect(screen.getByText('Version 1.0')).toBeTruthy()
    })
    // Version row visible, leaves still hidden — still no leaf-only actions.
    expect(screen.queryByRole('button', { name: /Modify/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Download archive' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Upload archive' })).toBeNull()

    fireEvent.click(screen.getByText('Version 1.0'))
    await waitFor(() => {
      expect(screen.getByText('data')).toBeTruthy()
    })
    // Leaf expanded → leaf-only actions appear.
    expect(screen.getAllByRole('button', { name: /Modify/ }).length).toBeGreaterThanOrEqual(1)
    expect(screen.getByRole('button', { name: 'Download archive' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Upload archive' })).toBeTruthy()
  })

  it('clicking delete on package volume (leaf) calls removePackageVolume', async () => {
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        effective_name: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('nginx'))
    await waitFor(() => {
      expect(screen.getByText('Version 1.0')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Version 1.0'))
    await waitFor(() => {
      expect(screen.getByText('data')).toBeTruthy()
    })
    // Leaf delete = the last destructive button in the DOM.
    const destructive = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    fireEvent.click(destructive[destructive.length - 1])
    await waitFor(() => {
      expect(screen.getByText('Delete Package Volume')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => {
      expect(mockRemovePackageVolume).toHaveBeenCalledWith('installed/core/nginx/1.0/data')
    })
  })

  it('clicking delete at the package level cascades via removePackageVolumeGroup', async () => {
    mockListFilesystems.mockResolvedValue({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        effective_name: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
          { name: '2.0/data', internal_name: 'installed/core/nginx/2.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    // Click the destructive button on the package row (the only destructive
    // button in the DOM right now because there are no user filesystems).
    const destructive = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    expect(destructive.length).toBe(1)
    fireEvent.click(destructive[0])
    await waitFor(() => {
      expect(screen.getByText('Delete Package Volumes')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => {
      expect(mockRemovePackageVolumeGroup).toHaveBeenCalledWith({
        repo: 'core',
        name: 'nginx',
        version: '',
        includeUninstalled: false,
      })
    })
  })

  it('clicking delete at the version level cascades with that version', async () => {
    mockListFilesystems.mockResolvedValue({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        effective_name: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
          { name: '2.0/data', internal_name: 'installed/core/nginx/2.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('nginx'))
    await waitFor(() => {
      expect(screen.getByText('Version 1.0')).toBeTruthy()
    })
    // Two version rows are visible now. Destructive buttons in the DOM:
    // [package-level, version 1.0, version 2.0]. Click version 2.0 (index 2).
    const destructive = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    expect(destructive.length).toBe(3)
    fireEvent.click(destructive[2])
    await waitFor(() => {
      expect(screen.getByText('Delete Package Volumes')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => {
      expect(mockRemovePackageVolumeGroup).toHaveBeenCalledWith({
        repo: 'core',
        name: 'nginx',
        version: '2.0',
        includeUninstalled: false,
      })
    })
  })

  it('cascade delete forwards the Show-uninstalled toggle as include_uninstalled', async () => {
    mockListFilesystems.mockResolvedValue({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'nginx',
        effective_name: 'nginx',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    // Flip the Show uninstalled toggle.
    fireEvent.click(screen.getByText('Show uninstalled volumes'))
    // Package-level delete.
    const destructive = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    fireEvent.click(destructive[0])
    await waitFor(() => {
      expect(screen.getByText('Delete Package Volumes')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => {
      expect(mockRemovePackageVolumeGroup).toHaveBeenCalledWith({
        repo: 'core',
        name: 'nginx',
        version: '',
        includeUninstalled: true,
      })
    })
  })

  it('cascade delete addresses dep packages by their effective (--dep--) name', async () => {
    mockListFilesystems.mockResolvedValue({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
    mockListPackageVolumes.mockResolvedValue([
      {
        package: 'jitsi/prosody',
        effective_name: 'jitsi--dep--prosody',
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/jitsi/subpackages/prosody/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    const destructive = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    fireEvent.click(destructive[0])
    await waitFor(() => {
      expect(screen.getByText('Delete Package Volumes')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => {
      expect(mockRemovePackageVolumeGroup).toHaveBeenCalledWith({
        repo: 'core',
        name: 'jitsi--dep--prosody',
        version: '',
        includeUninstalled: false,
      })
    })
  })
})

describe('volumeInternalName', () => {
  function volumeInternalName(vol) {
    const prefix = vol.state === 'installed' ? 'installed/' : 'uninstalled/'
    return prefix + vol.name
  }

  it('returns installed/ prefix for installed volumes', () => {
    expect(volumeInternalName({ name: 'core/nginx/1.0/data', state: 'installed' }))
      .toBe('installed/core/nginx/1.0/data')
  })

  it('returns uninstalled/ prefix for uninstalled volumes', () => {
    expect(volumeInternalName({ name: 'core/nginx/1.0/data', state: 'uninstalled' }))
      .toBe('uninstalled/core/nginx/1.0/data')
  })
})

describe('parseQuotaFromForm logic', () => {
  function parseQuotaFromForm(quotaValue, quotaUnit) {
    const UNITS = { B: 1, MB: 1024 * 1024, GB: 1024 * 1024 * 1024, TB: 1024 * 1024 * 1024 * 1024 }
    const raw = quotaValue ? parseFloat(quotaValue) : 0
    if (raw === 0) return 0
    return Math.round(raw * (UNITS[quotaUnit] || 1))
  }

  it('converts GB correctly', () => {
    expect(parseQuotaFromForm('2', 'GB')).toBe(2 * 1024 * 1024 * 1024)
  })

  it('converts MB correctly', () => {
    expect(parseQuotaFromForm('100', 'MB')).toBe(100 * 1024 * 1024)
  })

  it('converts TB correctly', () => {
    expect(parseQuotaFromForm('1', 'TB')).toBe(1024 * 1024 * 1024 * 1024)
  })

  it('converts B correctly', () => {
    expect(parseQuotaFromForm('512', 'B')).toBe(512)
  })

  it('returns 0 for zero value', () => {
    expect(parseQuotaFromForm('0', 'GB')).toBe(0)
  })

  it('returns 0 for empty string', () => {
    expect(parseQuotaFromForm('', 'GB')).toBe(0)
  })

  it('handles fractional GB values', () => {
    expect(parseQuotaFromForm('1.5', 'GB')).toBe(Math.round(1.5 * 1024 * 1024 * 1024))
  })

  it('handles fractional MB values', () => {
    expect(parseQuotaFromForm('0.5', 'MB')).toBe(Math.round(0.5 * 1024 * 1024))
  })

  it('handles fractional TB values', () => {
    expect(parseQuotaFromForm('0.25', 'TB')).toBe(Math.round(0.25 * 1024 * 1024 * 1024 * 1024))
  })
})

describe('StorageManagement swap panel', () => {
  beforeEach(() => {
    mockListFilesystems.mockImplementation(() =>
      Promise.resolve({ entries: [], has_more: false, total_pages: 1, total_count: 0 }),
    )
    mockListPackageVolumes.mockImplementation(() => Promise.resolve([]))
  })

  it('explains WHY a multi-disk pool has no swap, and names the layout', async () => {
    // The whole point of the panel: without it a user with several disks just
    // finds no swap and no reason. "Unsupported" alone would not be enough
    // either, so the device count and profile have to be on screen too.
    mockPing.mockImplementation(() =>
      Promise.resolve({
        swap: { supported: false, reason: 'multi_device', devices: 4, data_profiles: ['RAID5'], active: false },
      }),
    )
    renderStorageManagement()

    await waitFor(() => {
      expect(screen.getByText(/btrfs can only place a swapfile on a single-device filesystem/i)).toBeTruthy()
    })
    expect(screen.getByText(/4 device\(s\), data profile: RAID5/)).toBeTruthy()
  })

  it('reports usage when swap is active', async () => {
    mockPing.mockImplementation(() =>
      Promise.resolve({
        swap: {
          supported: true,
          devices: 1,
          data_profiles: ['single'],
          active: true,
          size_bytes: 2 * 1024 * 1024 * 1024,
          used_bytes: 512 * 1024 * 1024,
        },
      }),
    )
    renderStorageManagement()

    await waitFor(() => {
      expect(screen.getByText(/512.*MB.*\/.*2.*GB.*used/)).toBeTruthy()
    })
  })

  it('distinguishes supported-but-not-yet-active from unsupported', async () => {
    mockPing.mockImplementation(() =>
      Promise.resolve({
        swap: { supported: true, devices: 1, data_profiles: ['single'], active: false },
      }),
    )
    renderStorageManagement()

    await waitFor(() => {
      expect(screen.getByText('Set up, but not active yet')).toBeTruthy()
    })
    expect(screen.queryByText(/runs without swap/i)).toBeNull()
  })

  it('falls back to the probe-failed wording when reason is absent', async () => {
    // An unsupported capability with no reason must not render a raw
    // "storage.swap_reason_" key at the user.
    mockPing.mockImplementation(() =>
      Promise.resolve({ swap: { supported: false, devices: 0, active: false } }),
    )
    renderStorageManagement()

    await waitFor(() => {
      expect(screen.getByText(/storage layout could not be read/i)).toBeTruthy()
    })
  })

  it('renders nothing when the server reports no swap field', async () => {
    mockPing.mockImplementation(() => Promise.resolve({}))
    renderStorageManagement()

    await waitFor(() => expect(mockPing).toHaveBeenCalled())
    expect(screen.queryByText('Swap')).toBeNull()
  })
})
