import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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

// --- formatBytesSize tests ---

function formatBytesSize(bytes) {
  if (bytes === 0) return '0 bytes'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return `${gb} GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return `${mb} MB`
  return `${bytes} bytes`
}

describe('formatBytesSize', () => {
  it('returns "0 bytes" for 0', () => {
    expect(formatBytesSize(0)).toBe('0 bytes')
  })

  it('formats exact gigabytes', () => {
    expect(formatBytesSize(1073741824)).toBe('1 GB')
  })

  it('formats exact megabytes', () => {
    expect(formatBytesSize(20 * 1024 * 1024)).toBe('20 MB')
  })

  it('falls back to bytes for non-aligned values', () => {
    expect(formatBytesSize(12345)).toBe('12345 bytes')
  })
})

// --- formatDuration tests ---

function formatDuration(seconds) {
  if (seconds === 0) return '0 seconds'
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} hour${seconds / 3600 !== 1 ? 's' : ''}`
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} minute${seconds / 60 !== 1 ? 's' : ''}`
  return `${seconds} second${seconds !== 1 ? 's' : ''}`
}

describe('formatDuration', () => {
  it('returns "0 seconds" for 0', () => {
    expect(formatDuration(0)).toBe('0 seconds')
  })

  it('formats 1 second singular', () => {
    expect(formatDuration(1)).toBe('1 second')
  })

  it('formats multiple seconds', () => {
    expect(formatDuration(120)).toBe('2 minutes')
  })

  it('formats exact minutes', () => {
    expect(formatDuration(300)).toBe('5 minutes')
  })

  it('formats 1 minute singular', () => {
    expect(formatDuration(60)).toBe('1 minute')
  })

  it('formats exact hours', () => {
    expect(formatDuration(3600)).toBe('1 hour')
  })

  it('formats multiple hours', () => {
    expect(formatDuration(7200)).toBe('2 hours')
  })

  it('formats non-aligned seconds', () => {
    expect(formatDuration(90)).toBe('90 seconds')
  })

  it('formats non-aligned minutes as seconds', () => {
    expect(formatDuration(125)).toBe('125 seconds')
  })
})

// --- unitToBytes logic tests ---

describe('unitToBytes logic', () => {
  function unitToBytes(input, unit) {
    const num = Number(input)
    if (isNaN(num) || num < 0) return null
    if (unit === 'GB') return num * 1024 * 1024 * 1024
    if (unit === 'MB') return num * 1024 * 1024
    return num
  }

  it('converts GB to bytes', () => {
    expect(unitToBytes('1', 'GB')).toBe(1073741824)
  })

  it('converts MB to bytes', () => {
    expect(unitToBytes('1', 'MB')).toBe(1048576)
  })

  it('passes bytes through', () => {
    expect(unitToBytes('12345', 'bytes')).toBe(12345)
  })

  it('returns 0 for "0" input', () => {
    expect(unitToBytes('0', 'GB')).toBe(0)
  })

  it('returns null for negative input', () => {
    expect(unitToBytes('-1', 'GB')).toBe(null)
  })

  it('returns null for NaN input', () => {
    expect(unitToBytes('abc', 'GB')).toBe(null)
  })

  it('returns 0 for empty string (Number("") is 0)', () => {
    expect(unitToBytes('', 'GB')).toBe(0)
  })

  it('converts 50 GB correctly (default quota)', () => {
    expect(unitToBytes('50', 'GB')).toBe(53687091200)
  })

  it('converts 512 MB correctly', () => {
    expect(unitToBytes('512', 'MB')).toBe(536870912)
  })

  it('converts fractional GB', () => {
    expect(unitToBytes('1.5', 'GB')).toBe(1.5 * 1024 * 1024 * 1024)
  })

  it('converts fractional MB', () => {
    expect(unitToBytes('2.5', 'MB')).toBe(2.5 * 1024 * 1024)
  })
})

// --- timeoutToSeconds logic tests ---

describe('timeoutToSeconds logic', () => {
  function timeoutToSeconds(input, unit) {
    const num = Number(input)
    if (isNaN(num) || num < 0) return null
    if (unit === 'hours') return num * 3600
    if (unit === 'minutes') return num * 60
    return num
  }

  it('converts seconds directly', () => {
    expect(timeoutToSeconds('120', 'seconds')).toBe(120)
  })

  it('converts minutes to seconds', () => {
    expect(timeoutToSeconds('5', 'minutes')).toBe(300)
  })

  it('converts hours to seconds', () => {
    expect(timeoutToSeconds('2', 'hours')).toBe(7200)
  })

  it('returns 0 for "0" input', () => {
    expect(timeoutToSeconds('0', 'seconds')).toBe(0)
  })

  it('returns null for negative input', () => {
    expect(timeoutToSeconds('-1', 'seconds')).toBe(null)
  })

  it('returns null for NaN input', () => {
    expect(timeoutToSeconds('abc', 'seconds')).toBe(null)
  })
})

// --- Component rendering tests ---

const defaultSettings = {
  default_quota: '53687091200',
  max_archive_size: '1073741824',
  archive_unpack_timeout: '600',
  proton_image: '',
  monitoring_backend: 'uplot',
}

const mockGetSettings = vi.fn(() => Promise.resolve({ ...defaultSettings }))
const mockSetSetting = vi.fn(() => Promise.resolve())
const mockGetSetting = vi.fn(() => Promise.resolve('53687091200'))
const mockMonitoringStatus = vi.fn(() =>
  Promise.resolve({
    backend: 'uplot',
    prometheus: true,
    node_exporter: true,
    monitoring_ui: true,
  }),
)
const mockGetLocales = vi.fn(() => Promise.resolve({
  current: 'en-US',
  populated: ['en-US'],
  common_languages: [{ code: 'en-US', native_name: 'English', english_name: 'English' }],
  extended_locales: [],
}))

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    getSettings: mockGetSettings,
    setSetting: mockSetSetting,
    getSetting: mockGetSetting,
    getLocales: mockGetLocales,
    monitoringStatus: mockMonitoringStatus,
  }),
}))

const { mockToast } = vi.hoisted(() => ({
  mockToast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
    loading: vi.fn(),
    dismiss: vi.fn(),
  },
}))

vi.mock('sonner', () => ({ toast: mockToast }))

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
    mockMonitoringStatus.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
    mockMonitoringStatus.mockImplementation(() =>
      Promise.resolve({
        backend: 'uplot',
        prometheus: true,
        node_exporter: true,
        monitoring_ui: true,
      }),
    )
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

  it('renders six Save buttons', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const buttons = screen.getAllByRole('button', { name: 'Save' })
      expect(buttons).toHaveLength(6)
    })
  })

  it('renders quota input field', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Quota')).toBeTruthy()
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
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, default_quota: '0' })
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/0 \(no quota\)/)).toBeTruthy()
    })
  })

  it('renders the unit options GB, MB, bytes for quota', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const select = screen.getByLabelText('Quota').closest('form').querySelector('select')
      const options = select.querySelectorAll('option')
      const values = Array.from(options).map((o) => o.value)
      expect(values).toContain('GB')
      expect(values).toContain('MB')
      expect(values).toContain('bytes')
    })
  })

  it('initializes with MB unit when settings value is MB-aligned', async () => {
    // 512 MB = 536870912 bytes — should decompose to 512 MB
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, default_quota: '536870912' })
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Quota').value).toBe('512')
    })
    // Current value should display as MB
    expect(screen.getByText(/512 MB/)).toBeTruthy()
  })

  it('shows "0 (no quota)" when the input is set to 0', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, default_quota: '0' })
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/0 \(no quota\)/)).toBeTruthy()
    })
    await waitFor(() => {
      const input = screen.getByLabelText('Quota')
      expect(input.value).toBe('0')
    })
  })
})

// --- Max Archive Size section tests ---

describe('SystemSettings Max Archive Size', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
  })

  it('renders the Max Archive Size section', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('Max Archive Size')).toBeTruthy()
    })
  })

  it('renders archive size input field', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Size')).toBeTruthy()
    })
  })

  it('displays current archive size value', async () => {
    renderSystemSettings()
    await waitFor(() => {
      // 1073741824 = 1 GB
      expect(screen.getByText(/1 GB/)).toBeTruthy()
    })
  })

  it('initializes archive size input to 1 GB', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const input = screen.getByLabelText('Size')
      expect(input.value).toBe('1')
    })
  })

  it('displays archive size in GB when value is GB-aligned', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, max_archive_size: '1073741824' })
    renderSystemSettings()
    await waitFor(() => {
      const input = screen.getByLabelText('Size')
      expect(input.value).toBe('1')
    })
  })

  it('displays 0 bytes for zero archive size', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, max_archive_size: '0' })
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/0 bytes/)).toBeTruthy()
    })
  })

  it('renders description text for archive size', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/maximum file size allowed for archive uploads/)).toBeTruthy()
    })
  })
})

// --- Archive Unpack Timeout section tests ---

describe('SystemSettings Archive Unpack Timeout', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
  })

  it('renders the Archive Unpack Timeout section', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('Archive Unpack Timeout')).toBeTruthy()
    })
  })

  it('renders timeout input field', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Timeout')).toBeTruthy()
    })
  })

  it('displays current timeout value', async () => {
    renderSystemSettings()
    await waitFor(() => {
      // 600 seconds = 10 minutes
      expect(screen.getByText(/10 minutes/)).toBeTruthy()
    })
  })

  it('initializes timeout input to 10 minutes', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const input = screen.getByLabelText('Timeout')
      expect(input.value).toBe('10')
    })
  })

  it('renders timeout unit options seconds, minutes, hours', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const select = screen.getByLabelText('Timeout').closest('form').querySelector('select')
      const options = select.querySelectorAll('option')
      const values = Array.from(options).map((o) => o.value)
      expect(values).toContain('seconds')
      expect(values).toContain('minutes')
      expect(values).toContain('hours')
    })
  })

  it('decomposes hours correctly', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, archive_unpack_timeout: '3600' })
    renderSystemSettings()
    await waitFor(() => {
      const input = screen.getByLabelText('Timeout')
      expect(input.value).toBe('1')
    })
  })

  it('decomposes non-aligned seconds', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, archive_unpack_timeout: '90' })
    renderSystemSettings()
    await waitFor(() => {
      const input = screen.getByLabelText('Timeout')
      expect(input.value).toBe('90')
    })
  })

  it('displays 0 seconds for zero timeout', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, archive_unpack_timeout: '0' })
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/0 seconds/)).toBeTruthy()
    })
  })

  it('renders description text for timeout', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/maximum time allowed for unpacking/)).toBeTruthy()
    })
  })
})

// --- Proton Runner Image section tests ---

describe('SystemSettings Proton Runner Image', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
  })

  it('renders the Proton Runner Image section title', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('Proton Runner Image')).toBeTruthy()
    })
  })

  it('renders the image input field', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Image')).toBeTruthy()
    })
  })

  it('displays "not set" when proton_image is empty', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('not set')).toBeTruthy()
    })
  })

  it('displays configured value when proton_image is set', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, proton_image: 'ghcr.io/town-os/proton-runner:latest' })
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('ghcr.io/town-os/proton-runner:latest')).toBeTruthy()
    })
  })

  it('initializes input to empty string when not set', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const input = screen.getByLabelText('Image')
      expect(input.value).toBe('')
    })
  })

  it('initializes input with configured value', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, proton_image: 'my-registry/proton:v1' })
    renderSystemSettings()
    await waitFor(() => {
      const input = screen.getByLabelText('Image')
      expect(input.value).toBe('my-registry/proton:v1')
    })
  })

  it('allows saving empty string to clear the setting', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, proton_image: 'old-image:v1' })
    renderSystemSettings()
    await waitFor(() => {
      const buttons = screen.getAllByRole('button', { name: 'Save' })
      expect(buttons).toHaveLength(5)
    })
    await waitFor(() => {
      const input = screen.getByLabelText('Image')
      expect(input.value).toBe('old-image:v1')
    })
    const input = screen.getByLabelText('Image')
    fireEvent.change(input, { target: { value: '' } })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[4])
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('proton_image', '')
    })
  })

  it('renders description text', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText(/Proton compatibility layer/)).toBeTruthy()
    })
  })
})

describe('SystemSettings Monitoring Backend', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
  })

  it('renders the Monitoring Dashboard section title', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('Monitoring Dashboard')).toBeTruthy()
    })
  })

  it('renders the backend selector', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Backend')).toBeTruthy()
    })
  })

  it('displays current backend value as uPlot by default', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const strong = screen.getByText('uPlot')
      expect(strong.tagName).toBe('STRONG')
    })
  })

  it('displays Grafana when monitoring_backend is grafana', async () => {
    mockGetSettings.mockResolvedValueOnce({ ...defaultSettings, monitoring_backend: 'grafana' })
    renderSystemSettings()
    await waitFor(() => {
      const strong = screen.getAllByText('Grafana')
      expect(strong.length).toBeGreaterThan(0)
    })
  })

  it('renders both uplot and grafana options', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const select = screen.getByLabelText('Backend')
      const options = select.querySelectorAll('option')
      expect(options).toHaveLength(2)
      expect(options[0].value).toBe('uplot')
      expect(options[1].value).toBe('grafana')
    })
  })
})

// This block reclaims the coverage that got deleted when the unchanged-save
// no-op feature landed — those tests clicked Save without changing the input,
// which under the new behaviour is (correctly) a no-op. These tests change
// the input via userEvent (which, unlike fireEvent.change, correctly drives
// React's controlled-input value tracker for number inputs and selects in
// jsdom) and assert that setSetting is called with the new value.
describe('SystemSettings changed-value save hits setSetting', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockMonitoringStatus.mockClear()
    mockGetLocales.mockClear()
    mockToast.info.mockClear()
    mockToast.success.mockClear()
    mockToast.loading.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
    mockGetLocales.mockImplementation(() => Promise.resolve({
      current: 'en-US',
      populated: ['en-US', 'es-ES'],
      common_languages: [
        { code: 'en-US', native_name: 'English', english_name: 'English' },
        { code: 'es-ES', native_name: 'Español', english_name: 'Spanish' },
      ],
      extended_locales: [],
    }))
  })

  // NOTE: a "type a new number into the Quota field" happy-path test is
  // deliberately absent. user-event v14 + jsdom + React 19 can't reliably
  // clear a pre-filled controlled <input type="number"> that starts at
  // '50' — neither user.clear() nor {selectall}{backspace} nor the native
  // value-setter-plus-input-event pattern consistently empties it.
  // Archive Size (initial '1') and Timeout (initial '10') use the same
  // user.clear()+user.type() pattern successfully, so the bug is
  // value-specific, not pattern-specific, and worth leaving as a failing
  // test would pin us to that quirk. The unit-change test below exercises
  // the same handleSaveQuota code path (quotaToBytes -> setSetting with a
  // new byte count) via selectOptions on the unit select, which is rock
  // solid in the same stack.

  it('quota: changing unit to MB re-computes bytes and posts them', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Quota').value).toBe('50')
    })
    // Loaded is 50 GB (53687091200 bytes). Switching the unit select to
    // MB makes the form compute 50 MB (52428800 bytes), which is a real
    // change, so setSetting fires with the new byte count.
    const quotaUnit = document.getElementById('quota-unit')
    await user.selectOptions(quotaUnit, 'MB')
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[1])
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('default_quota', String(50 * 1024 * 1024))
    })
  })

  it('archive size: changing value posts the new byte count', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Size').value).toBe('1')
    })
    const input = screen.getByLabelText('Size')
    await user.clear(input)
    await user.type(input, '5')
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[2])
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('max_archive_size', String(5 * 1024 * 1024 * 1024))
    })
  })

  it('timeout: changing value posts the new second count', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Timeout').value).toBe('10')
    })
    const input = screen.getByLabelText('Timeout')
    await user.clear(input)
    await user.type(input, '30')
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[3])
    await waitFor(() => {
      // Default unit is minutes when the loaded value is minute-aligned,
      // so 30 minutes = 1800 seconds.
      expect(mockSetSetting).toHaveBeenCalledWith('archive_unpack_timeout', '1800')
    })
  })

  it('proton image: changing value posts the new string', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Image').value).toBe('')
    })
    const input = screen.getByLabelText('Image')
    await user.type(input, 'ghcr.io/town-os/proton-runner:v2')
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[4])
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('proton_image', 'ghcr.io/town-os/proton-runner:v2')
    })
  })

  it('language: changing selected locale posts it', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Language').value).toBe('en-US')
    })
    const select = screen.getByLabelText('Language')
    await user.selectOptions(select, 'es-ES')
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[0])
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('locale', 'es-ES')
    })
  })

  it('monitoring: changing backend posts it and polls monitoringStatus', async () => {
    const user = userEvent.setup()
    mockMonitoringStatus.mockImplementation(() =>
      Promise.resolve({
        backend: 'grafana',
        prometheus: true,
        node_exporter: true,
        grafana: true,
        monitoring_ui: true,
      }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Backend').value).toBe('uplot')
    })
    const select = screen.getByLabelText('Backend')
    await user.selectOptions(select, 'grafana')
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[saveButtons.length - 1])
    })
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('monitoring_backend', 'grafana')
    })
    await waitFor(() => {
      expect(mockMonitoringStatus).toHaveBeenCalled()
    })
  })
})

describe('SystemSettings unchanged save is a no-op', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockMonitoringStatus.mockClear()
    mockGetLocales.mockClear()
    mockToast.info.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
    mockGetLocales.mockImplementation(() => Promise.resolve({
      current: 'en-US',
      populated: ['en-US'],
      common_languages: [{ code: 'en-US', native_name: 'English', english_name: 'English' }],
      extended_locales: [],
    }))
  })

  function expectNothingToDoToast() {
    expect(mockToast.info).toHaveBeenCalledTimes(1)
    expect(mockToast.info).toHaveBeenCalledWith('There is nothing to be done')
  }

  it('language: clicking Save with unchanged locale does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const select = screen.getByLabelText('Language')
      expect(select.value).toBe('en-US')
    })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[0])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('quota: clicking Save with unchanged value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Quota').value).toBe('50')
    })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[1])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('archive size: clicking Save with unchanged value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Size').value).toBe('1')
    })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[2])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('timeout: clicking Save with unchanged value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Timeout').value).toBe('10')
    })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[3])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('proton image: clicking Save with unchanged empty value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Image').value).toBe('')
    })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[4])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('proton image: clicking Save with unchanged non-empty value does not call setSetting', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, proton_image: 'ghcr.io/town-os/proton-runner:latest' }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Image').value).toBe('ghcr.io/town-os/proton-runner:latest')
    })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[4])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('monitoring: clicking Save with unchanged backend does not call setSetting, monitoringStatus, or toggle saving state', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Backend').value).toBe('uplot')
    })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    await act(async () => {
      fireEvent.click(saveButtons[saveButtons.length - 1])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expect(mockMonitoringStatus).not.toHaveBeenCalled()
    expectNothingToDoToast()
    // Button stays as "Save" — never transitions to "Saving..."
    expect(screen.queryByRole('button', { name: 'Saving...' })).toBeNull()
    expect(screen.getByLabelText('Backend').disabled).toBe(false)
  })

})

describe('formatBytes edge cases', () => {
  it('formats very large values (1000 GB)', () => {
    expect(formatBytes(1000 * 1024 * 1024 * 1024)).toBe('1000 GB')
  })

  it('formats 1 GB minus 1 byte as bytes (not an exact GB or MB boundary)', () => {
    // 1 GB - 1 byte = 1073741823
    // 1073741823 / (1024*1024*1024) = 0.9999999990686774... (not integer, but >= 1 is false for gb path)
    // 1073741823 / (1024*1024) = 1023.9999990463257... (not integer)
    // So it falls back to bytes
    const value = 1024 * 1024 * 1024 - 1
    expect(formatBytes(value)).toBe(`${value} bytes`)
  })
})
