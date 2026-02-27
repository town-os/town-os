import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// --- Pure function tests (mirrored from SystemSettings.jsx) ---

function formatBytes(bytes) {
  if (bytes === 0) return '0 (no quota)'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return `${gb} GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return `${mb} MB`
  return `${bytes} bytes`
}

describe('formatBytes', () => {
  it('returns "0 (no quota)" for 0', () => {
    expect(formatBytes(0)).toBe('0 (no quota)')
  })

  it('formats exact gigabytes', () => {
    expect(formatBytes(1073741824)).toBe('1 GB')
  })

  it('formats multiple gigabytes', () => {
    expect(formatBytes(50 * 1024 * 1024 * 1024)).toBe('50 GB')
  })

  it('formats exact megabytes', () => {
    expect(formatBytes(1048576)).toBe('1 MB')
  })

  it('formats multiple megabytes', () => {
    expect(formatBytes(512 * 1024 * 1024)).toBe('512 MB')
  })

  it('falls back to bytes for non-aligned values', () => {
    expect(formatBytes(12345)).toBe('12345 bytes')
  })

  it('falls back to bytes for fractional MB', () => {
    // 1.5 MB
    expect(formatBytes(1572864)).toBe('1572864 bytes')
  })

  it('formats fractional GB as MB when it is an exact MB value', () => {
    // 1.5 GB = 1536 MB (integer MB), so formatBytes returns MB
    expect(formatBytes(1610612736)).toBe('1536 MB')
  })

  it('formats 100 GB', () => {
    expect(formatBytes(100 * 1024 * 1024 * 1024)).toBe('100 GB')
  })

  it('formats 1 byte as bytes', () => {
    expect(formatBytes(1)).toBe('1 bytes')
  })
})

// --- quotaToBytes logic tests ---

describe('quotaToBytes logic', () => {
  function quotaToBytes(quotaInput, quotaUnit) {
    const num = Number(quotaInput)
    if (isNaN(num) || num < 0) return null
    if (quotaUnit === 'GB') return num * 1024 * 1024 * 1024
    if (quotaUnit === 'MB') return num * 1024 * 1024
    return num
  }

  it('converts GB to bytes', () => {
    expect(quotaToBytes('1', 'GB')).toBe(1073741824)
  })

  it('converts MB to bytes', () => {
    expect(quotaToBytes('1', 'MB')).toBe(1048576)
  })

  it('passes bytes through', () => {
    expect(quotaToBytes('12345', 'bytes')).toBe(12345)
  })

  it('returns 0 for "0" input', () => {
    expect(quotaToBytes('0', 'GB')).toBe(0)
  })

  it('returns null for negative input', () => {
    expect(quotaToBytes('-1', 'GB')).toBe(null)
  })

  it('returns null for NaN input', () => {
    expect(quotaToBytes('abc', 'GB')).toBe(null)
  })

  it('returns 0 for empty string (Number("") is 0)', () => {
    // Number('') === 0, which is not NaN and not negative, so returns 0 * multiplier = 0
    expect(quotaToBytes('', 'GB')).toBe(0)
  })

  it('converts 50 GB correctly (default quota)', () => {
    expect(quotaToBytes('50', 'GB')).toBe(53687091200)
  })

  it('converts 512 MB correctly', () => {
    expect(quotaToBytes('512', 'MB')).toBe(536870912)
  })

  it('converts fractional GB', () => {
    expect(quotaToBytes('1.5', 'GB')).toBe(1.5 * 1024 * 1024 * 1024)
  })

  it('converts fractional MB', () => {
    expect(quotaToBytes('2.5', 'MB')).toBe(2.5 * 1024 * 1024)
  })
})

// --- Component rendering tests ---

const mockGetSettings = vi.fn(() =>
  Promise.resolve({ default_quota: '53687091200' }),
)
const mockSetSetting = vi.fn(() => Promise.resolve())
const mockGetSetting = vi.fn(() => Promise.resolve('53687091200'))

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    getSettings: mockGetSettings,
    setSetting: mockSetSetting,
    getSetting: mockGetSetting,
  }),
}))

import SystemSettings from './SystemSettings.jsx'

function renderSystemSettings() {
  return render(
    <MemoryRouter>
      <SystemSettings />
    </MemoryRouter>,
  )
}

describe('SystemSettings component', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
  })

  it('renders the System Settings heading', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('System Settings')).toBeTruthy()
    })
  })

  it('renders the subheading', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('System-wide defaults for all users')).toBeTruthy()
    })
  })

  it('renders the Default Volume Quota section', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('Default Volume Quota')).toBeTruthy()
    })
  })

  it('renders Save button', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy()
    })
  })

  it('renders quota input field', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Quota')).toBeTruthy()
    })
  })

  it('renders unit selector', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Unit')).toBeTruthy()
    })
  })

  it('calls getSettings on mount', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(mockGetSettings).toHaveBeenCalled()
    })
  })

  it('displays current quota value', async () => {
    renderSystemSettings()
    await waitFor(() => {
      // 53687091200 = 50 GB
      expect(screen.getByText(/50 GB/)).toBeTruthy()
    })
  })

  it('renders zero quota explanation text', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/Set to 0 for no quota limit/)).toBeTruthy()
    })
  })

  it('shows no quota when settings return 0', async () => {
    mockGetSettings.mockResolvedValueOnce({ default_quota: '0' })
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/0 \(no quota\)/)).toBeTruthy()
    })
  })

  it('renders the unit options GB, MB, bytes', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const select = screen.getByLabelText('Unit')
      const options = select.querySelectorAll('option')
      const values = Array.from(options).map((o) => o.value)
      expect(values).toContain('GB')
      expect(values).toContain('MB')
      expect(values).toContain('bytes')
    })
  })
})
