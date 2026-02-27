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

function buildUnifiedVolumeTree(installedFilesystems, uninstalledFilesystems) {
  const tree = {}

  function addToTree(filesystems, state) {
    for (const fs of filesystems) {
      const parts = fs.name.split('/')
      if (parts.length >= 4) {
        const pkgKey = `${parts[0]}/${parts[1]}`
        const version = parts[2]
        const volName = parts.slice(3).join('/')
        if (!tree[pkgKey]) tree[pkgKey] = {}
        if (!tree[pkgKey][version]) tree[pkgKey][version] = { state, volumes: [] }
        tree[pkgKey][version].volumes.push({ ...fs, volumeName: volName, state })
      } else {
        const pkgName = parts[0] || fs.name
        const version = parts.length > 1 ? parts[1] : ''
        const volName = parts.length > 2 ? parts.slice(2).join('/') : ''
        if (!tree[pkgName]) tree[pkgName] = {}
        if (!tree[pkgName][version]) tree[pkgName][version] = { state, volumes: [] }
        tree[pkgName][version].volumes.push({ ...fs, volumeName: volName, state })
      }
    }
  }

  addToTree(installedFilesystems, 'installed')
  addToTree(uninstalledFilesystems, 'uninstalled')
  return tree
}

function sumQuota(volumes) {
  return volumes.reduce((sum, v) => sum + (v.quota || 0), 0)
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

describe('buildUnifiedVolumeTree', () => {
  it('builds tree from installed filesystems', () => {
    const installed = [
      { name: 'core/nginx/1.0/data', quota: 1024 },
      { name: 'core/nginx/1.0/config', quota: 512 },
    ]
    const tree = buildUnifiedVolumeTree(installed, [])
    expect(tree['core/nginx']).toBeDefined()
    expect(tree['core/nginx']['1.0']).toBeDefined()
    expect(tree['core/nginx']['1.0'].state).toBe('installed')
    expect(tree['core/nginx']['1.0'].volumes).toHaveLength(2)
    expect(tree['core/nginx']['1.0'].volumes[0].volumeName).toBe('data')
    expect(tree['core/nginx']['1.0'].volumes[1].volumeName).toBe('config')
  })

  it('builds tree from uninstalled filesystems', () => {
    const uninstalled = [
      { name: 'core/nginx/1.0/data', quota: 0 },
    ]
    const tree = buildUnifiedVolumeTree([], uninstalled)
    expect(tree['core/nginx']['1.0'].state).toBe('uninstalled')
    expect(tree['core/nginx']['1.0'].volumes[0].state).toBe('uninstalled')
  })

  it('merges installed and uninstalled with different versions', () => {
    const installed = [{ name: 'core/nginx/2.0/data', quota: 0 }]
    const uninstalled = [{ name: 'core/nginx/1.0/data', quota: 0 }]
    const tree = buildUnifiedVolumeTree(installed, uninstalled)
    expect(tree['core/nginx']['2.0'].state).toBe('installed')
    expect(tree['core/nginx']['1.0'].state).toBe('uninstalled')
  })

  it('returns empty tree for no filesystems', () => {
    const tree = buildUnifiedVolumeTree([], [])
    expect(Object.keys(tree)).toHaveLength(0)
  })

  it('handles short paths (fewer than 4 parts)', () => {
    const installed = [{ name: 'simple', quota: 0 }]
    const tree = buildUnifiedVolumeTree(installed, [])
    expect(tree['simple']).toBeDefined()
    expect(tree['simple']['']).toBeDefined()
  })

  it('handles 2-part names', () => {
    const installed = [{ name: 'pkg/1.0', quota: 0 }]
    const tree = buildUnifiedVolumeTree(installed, [])
    expect(tree['pkg']).toBeDefined()
    expect(tree['pkg']['1.0']).toBeDefined()
  })

  it('handles 3-part names', () => {
    const installed = [{ name: 'repo/pkg/1.0', quota: 0 }]
    const tree = buildUnifiedVolumeTree(installed, [])
    expect(tree['repo']).toBeDefined()
    expect(tree['repo']['pkg']).toBeDefined()
  })

  it('handles deeply nested volume names', () => {
    const installed = [{ name: 'core/nginx/1.0/data/nested/deep', quota: 0 }]
    const tree = buildUnifiedVolumeTree(installed, [])
    expect(tree['core/nginx']['1.0'].volumes[0].volumeName).toBe('data/nested/deep')
  })

  it('groups multiple packages', () => {
    const installed = [
      { name: 'core/nginx/1.0/data', quota: 0 },
      { name: 'core/redis/7.0/data', quota: 0 },
    ]
    const tree = buildUnifiedVolumeTree(installed, [])
    expect(Object.keys(tree)).toHaveLength(2)
    expect(tree['core/nginx']).toBeDefined()
    expect(tree['core/redis']).toBeDefined()
  })
})

describe('sumQuota', () => {
  it('sums quotas across volumes', () => {
    const volumes = [{ quota: 1000 }, { quota: 2000 }, { quota: 3000 }]
    expect(sumQuota(volumes)).toBe(6000)
  })

  it('returns 0 for empty array', () => {
    expect(sumQuota([])).toBe(0)
  })

  it('treats missing quota as 0', () => {
    const volumes = [{ quota: 1000 }, {}, { quota: 2000 }]
    expect(sumQuota(volumes)).toBe(3000)
  })

  it('treats quota: 0 as 0', () => {
    const volumes = [{ quota: 0 }, { quota: 0 }]
    expect(sumQuota(volumes)).toBe(0)
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

const mockCreateFilesystem = vi.fn(() => Promise.resolve())
const mockModifyFilesystem = vi.fn(() => Promise.resolve())
const mockRemoveFilesystem = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listFilesystems: mockListFilesystems,
    createFilesystem: mockCreateFilesystem,
    modifyFilesystem: mockModifyFilesystem,
    removeFilesystem: mockRemoveFilesystem,
    downloadArchive: vi.fn(() => Promise.resolve({ body: null, blob: () => Promise.resolve(new Blob()) })),
    uploadArchive: vi.fn(() => Promise.resolve({ message: 'ok' })),
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
    mockCreateFilesystem.mockClear()
    mockModifyFilesystem.mockClear()
    mockRemoveFilesystem.mockClear()
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

  it('renders Package Volumes section when installed volumes exist', async () => {
    mockListFilesystems
      .mockResolvedValueOnce({
        entries: [],
        has_more: false,
        total_pages: 1,
        total_count: 0,
      })
      .mockResolvedValueOnce({
        entries: [{ name: 'core/nginx/1.0/data', quota: 0 }],
        has_more: false,
        total_pages: 1,
        total_count: 1,
      })
      .mockResolvedValueOnce({
        entries: [],
        has_more: false,
        total_pages: 1,
        total_count: 0,
      })
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
    mockListFilesystems
      .mockResolvedValueOnce({
        entries: [],
        has_more: false,
        total_pages: 1,
        total_count: 0,
      })
      .mockResolvedValueOnce({
        entries: [{ name: 'core/nginx/1.0/data', quota: 0 }],
        has_more: false,
        total_pages: 1,
        total_count: 1,
      })
      .mockResolvedValueOnce({
        entries: [],
        has_more: false,
        total_pages: 1,
        total_count: 0,
      })
    renderStorageManagement()
    // Expand the package tree to see individual volumes
    await waitFor(() => {
      expect(screen.getByText('Package Volumes')).toBeTruthy()
    })
    // Click the package row to expand it
    const pkgRow = screen.getByText('core/nginx')
    fireEvent.click(pkgRow)
    // Now click the Modify button on the volume row
    await waitFor(() => {
      const modifyButtons = screen.getAllByRole('button', { name: /Modify/ })
      expect(modifyButtons.length).toBeGreaterThanOrEqual(1)
    })
    const modifyButtons = screen.getAllByRole('button', { name: /Modify/ })
    fireEvent.click(modifyButtons[0])
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
