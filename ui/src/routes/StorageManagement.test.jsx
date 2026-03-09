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
const mockCreateFilesystem = vi.fn(() => Promise.resolve())
const mockModifyFilesystem = vi.fn(() => Promise.resolve())
const mockRemoveFilesystem = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listFilesystems: mockListFilesystems,
    listPackageVolumes: mockListPackageVolumes,
    removePackageVolume: mockRemovePackageVolume,
    createFilesystem: mockCreateFilesystem,
    modifyFilesystem: mockModifyFilesystem,
    removeFilesystem: mockRemoveFilesystem,
    downloadArchive: vi.fn(() => Promise.resolve({ body: null, blob: () => Promise.resolve(new Blob()) })),
    uploadArchive: vi.fn(() => Promise.resolve({ message: 'ok' })),
    getSetting: vi.fn(() => Promise.resolve('53687091200')),
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
        repo: 'core',
        volumes: [
          { name: '1.0/data', internal_name: 'installed/core/nginx/1.0/data', repo: 'core', quota: 0, state: 'installed' },
        ],
      },
    ])
    renderStorageManagement()
    // Expand the package tree to see individual volumes
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    // Click the package row to expand it
    const pkgRow = screen.getByText('nginx')
    fireEvent.click(pkgRow)
    // Now click the Modify button on the volume row
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

  it('renders delete button for package volumes', async () => {
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
    // Expand the package
    fireEvent.click(screen.getByText('nginx'))
    await waitFor(() => {
      expect(screen.getByText('1.0/data')).toBeTruthy()
    })
    // Should have a delete button in the package volume row
    const deleteButtons = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    // At least 2: one for user filesystem, one for package volume
    expect(deleteButtons.length).toBeGreaterThanOrEqual(2)
  })

  it('clicking delete on package volume shows confirm dialog and calls removePackageVolume', async () => {
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
    // Expand the package
    fireEvent.click(screen.getByText('nginx'))
    await waitFor(() => {
      expect(screen.getByText('1.0/data')).toBeTruthy()
    })
    // Find all destructive buttons; the last one should be in the package volume row
    const deleteButtons = screen.getAllByRole('button').filter(
      (btn) => btn.classList.contains('text-destructive'),
    )
    // Click the last destructive button (the package volume delete)
    fireEvent.click(deleteButtons[deleteButtons.length - 1])
    // Confirm dialog should appear with package volume title
    await waitFor(() => {
      expect(screen.getByText('Delete Package Volume')).toBeTruthy()
    })
    // Click the confirm button
    const confirmButton = screen.getByRole('button', { name: 'Delete' })
    fireEvent.click(confirmButton)
    await waitFor(() => {
      expect(mockRemovePackageVolume).toHaveBeenCalledWith('installed/core/nginx/1.0/data')
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
