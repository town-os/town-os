import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { I18nProvider } from '@/i18n/I18nContext.jsx'
import enUS from '@/i18n/en-US.js'
import esES from '@/i18n/es-ES.js'
import deDE from '@/i18n/de-DE.js'
import deAT from '@/i18n/de-AT.js'
import frFR from '@/i18n/fr-FR.js'

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

// --- Language preselection rule (mirrored from SystemSettings.jsx) ---

// Which of the two "current" languages the dropdown should open on: the locale
// the page is rendered in (browser-detected or previously chosen, held by
// I18nContext) or the server's global `locale` setting. The page one wins
// whenever the server offers it at all; `current` is only a fallback so the
// control never ends up holding a value none of its options carry.
describe('language preselection rule', () => {
  function listsLocale(list, code) {
    return Array.isArray(list) && list.some((l) => l.code === code)
  }

  function preselect(data, locale) {
    const inCommon = listsLocale(data.common_languages, locale)
    const inExtended = listsLocale(data.extended_locales, locale)
    return {
      selected: inCommon || inExtended ? locale : data.current,
      expand: !inCommon && inExtended,
    }
  }

  const data = {
    current: 'en-US',
    common_languages: [{ code: 'en-US' }, { code: 'es-ES' }],
    extended_locales: [{ code: 'de-AT' }],
  }

  it('prefers the page locale over the server setting', () => {
    expect(preselect(data, 'es-ES').selected).toBe('es-ES')
  })

  it('keeps the page locale when it equals the server setting', () => {
    expect(preselect(data, 'en-US').selected).toBe('en-US')
  })

  it('accepts a page locale from the extended list', () => {
    expect(preselect(data, 'de-AT').selected).toBe('de-AT')
  })

  it('expands the extended list only for a locale that lives there', () => {
    expect(preselect(data, 'de-AT').expand).toBe(true)
    expect(preselect(data, 'es-ES').expand).toBe(false)
    expect(preselect(data, 'fr-FR').expand).toBe(false)
  })

  it('falls back to the server setting for an unlisted page locale', () => {
    expect(preselect(data, 'fr-FR').selected).toBe('en-US')
  })

  it('tolerates missing lists', () => {
    expect(preselect({ current: 'en-US' }, 'es-ES').selected).toBe('en-US')
    expect(listsLocale(undefined, 'en-US')).toBe(false)
  })
})

// --- Component rendering tests ---

const defaultSettings = {
  default_quota: '53687091200',
  max_archive_size: '1073741824',
  archive_unpack_timeout: '600',
  peer_ttl: '7200',
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

const mockPing = vi.fn(() => Promise.resolve({ proton_enabled: true }))
const mockDnsStatus = vi.fn(() =>
  Promise.resolve({
    enabled: true,
    running: true,
    tld: 'home',
    record_count: 0,
    local_forwarders: false,
    forwarders: ['8.8.8.8:53', '8.8.4.4:53'],
  }),
)

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    getSettings: mockGetSettings,
    setSetting: mockSetSetting,
    getSetting: mockGetSetting,
    getLocales: mockGetLocales,
    monitoringStatus: mockMonitoringStatus,
    ping: mockPing,
    dnsStatus: mockDnsStatus,
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

// Put a value into a controlled number field.
//
// React drives onChange for text-like inputs off the 'input' event, and a
// 'change' event alone has been observed to leave this stack's controlled number
// inputs untouched -- the field keeps its old value, the form saves nothing, and
// the test reads as "the save never fired" rather than "the typing never landed".
// user.clear()+user.type() is no better here (see the NOTE below). Fire input,
// fall back to change, and then ASSERT the field actually holds the value, so a
// failed interaction fails on its own line instead of impersonating a bug.
function setNumberField(label, value) {
  const el = screen.getByLabelText(label)
  fireEvent.input(el, { target: { value } })
  if (el.value !== String(value)) {
    fireEvent.change(el, { target: { value } })
  }
  expect(el.value).toBe(String(value))
  return el
}

// Click the Save button belonging to the section that owns `label`.
//
// These tests used to index into every Save button on the page
// (saveButtons[4]), which is a trap: the Language form renders only once
// getLocales resolves, so until it appears every index below it shifts by one
// and a click lands on the wrong form. A click on a neighbouring form saves an
// unchanged value, takes the nothing-to-do path, and calls nothing -- which is
// indistinguishable from the save under test never firing, and makes the
// "unchanged value does not save" tests pass for the wrong reason. Find the
// field, submit its own form.
function saveSection(label) {
  const form = screen.getByLabelText(label).closest('form')
  fireEvent.click(within(form).getByRole('button', { name: 'Save' }))
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

  it('renders seven Save buttons', async () => {
    renderSystemSettings()
    await waitFor(() => {
      const buttons = screen.getAllByRole('button', { name: 'Save' })
      expect(buttons).toHaveLength(8)
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
      expect(buttons).toHaveLength(8)
    })
    await waitFor(() => {
      const input = screen.getByLabelText('Image')
      expect(input.value).toBe('old-image:v1')
    })
    const input = screen.getByLabelText('Image')
    fireEvent.change(input, { target: { value: '' } })
    saveSection('Image')
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

  it('hides the proton card when ping reports proton_enabled=false', async () => {
    mockPing.mockResolvedValueOnce({ proton_enabled: false })
    renderSystemSettings()
    // Wait for something else to render so we know the page has loaded.
    await waitFor(() => {
      expect(screen.getByText(/Default Volume Quota/i)).toBeTruthy()
    })
    expect(screen.queryByLabelText('Image')).toBeNull()
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
    saveSection('Quota')
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
    saveSection('Size')
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
    saveSection('Timeout')
    await waitFor(() => {
      // Default unit is minutes when the loaded value is minute-aligned,
      // so 30 minutes = 1800 seconds.
      expect(mockSetSetting).toHaveBeenCalledWith('archive_unpack_timeout', '1800')
    })
  })

  it('peer ttl: changing value posts the new second count', async () => {
    renderSystemSettings()
    await waitFor(() => {
      // 7200s loads as 2 hours (hour-aligned).
      expect(screen.getByLabelText('TTL').value).toBe('2')
    })
    setNumberField('TTL', '3')
    // Submit the form the field actually belongs to. Indexing into every Save
    // button on the page makes the test depend on how many sections happen to
    // render above this one, and a click that lands on a neighbouring form saves
    // an unchanged value -- which looks exactly like the save never firing.
    saveSection('TTL')
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('peer_ttl', String(3 * 3600))
    })
  })

  // The unit select is half of the answer: the same number means a different
  // number of seconds depending on which unit is chosen.
  it('peer ttl: changing the unit re-computes the seconds and posts them', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('TTL').value).toBe('2')
    })
    // Loaded as 2 hours. Switching to minutes makes it 2 minutes -- a real
    // change, so it saves 120 seconds rather than 7200.
    await user.selectOptions(document.getElementById('peer-ttl-unit'), 'minutes')
    saveSection('TTL')
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('peer_ttl', String(2 * 60))
    })
  })

  // A TTL of zero would mean every peer is already expired. The field declares
  // min="1", so the browser refuses the submit outright and the save handler is
  // never even reached -- no request, and no error toast either, because there is
  // nothing for the app to complain about. Assert the guard that is actually doing
  // the work rather than one that cannot fire.
  it('peer ttl: zero is refused by the field itself and never posted', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('TTL').value).toBe('2')
    })
    const input = setNumberField('TTL', '0')
    expect(input.min).toBe('1')
    expect(input.checkValidity()).toBe(false)

    saveSection('TTL')
    await act(async () => {})
    expect(mockSetSetting).not.toHaveBeenCalled()
  })

  // An empty field, by contrast, satisfies the browser (nothing is `required`),
  // so it reaches the handler -- which has to reject it, since "" is not a TTL.
  it('peer ttl: an empty value is rejected and never posted', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('TTL').value).toBe('2')
    })
    setNumberField('TTL', '')
    saveSection('TTL')
    await waitFor(() => {
      expect(mockToast.error).toHaveBeenCalledWith('Invalid peer TTL value')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
  })

  // A stored value is decomposed into the largest unit it divides evenly into,
  // so the operator sees "30 minutes", not "1800 seconds".
  it('peer ttl: a minute-aligned stored value loads as minutes', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, peer_ttl: '1800' }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('TTL').value).toBe('30')
    })
    expect(document.getElementById('peer-ttl-unit').value).toBe('minutes')
  })

  it('peer ttl: a value aligned to nothing loads as seconds', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, peer_ttl: '90' }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('TTL').value).toBe('90')
    })
    expect(document.getElementById('peer-ttl-unit').value).toBe('seconds')
  })

  it('proton image: changing value posts the new string', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Image').value).toBe('')
    })
    const input = screen.getByLabelText('Image')
    await user.type(input, 'ghcr.io/town-os/proton-runner:v2')
    saveSection('Image')
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
    saveSection('Language')
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
    await act(async () => {
      saveSection('Backend')
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
    await act(async () => {
      saveSection('Language')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('quota: clicking Save with unchanged value does not call setSetting', async () => {
    renderSystemSettings()
    // The Quota field shows '50' by default even before settings load, so wait
    // for a value that only the loaded settings can produce.
    await waitFor(() => {
      expect(screen.getByLabelText('Quota').value).toBe('50')
    })
    await act(async () => {
      saveSection('Quota')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('archive size: clicking Save with unchanged value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Size').value).toBe('1')
    })
    await act(async () => {
      saveSection('Size')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('timeout: clicking Save with unchanged value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Timeout').value).toBe('10')
    })
    await act(async () => {
      saveSection('Timeout')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('peer ttl: clicking Save with unchanged value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('TTL').value).toBe('2')
    })
    await act(async () => {
      saveSection('TTL')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('proton image: clicking Save with unchanged empty value does not call setSetting', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Image').value).toBe('')
    })
    await act(async () => {
      saveSection('Image')
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
    await act(async () => {
      saveSection('Image')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expectNothingToDoToast()
  })

  it('monitoring: clicking Save with unchanged backend does not call setSetting, monitoringStatus, or toggle saving state', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Backend').value).toBe('uplot')
    })
    await act(async () => {
      saveSection('Backend')
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

describe('SystemSettings DNS resolution mode', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
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

  it('defaults to auto when the setting is absent', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Mode').value).toBe('auto')
    })
  })

  it('reflects the stored mode', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, dns_resolution_mode: 'forward' }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Mode').value).toBe('forward')
    })
  })

  it('saves the selected mode', async () => {
    const user = userEvent.setup()
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Mode').value).toBe('auto')
    })
    await user.selectOptions(screen.getByLabelText('Mode'), 'forward')
    await act(async () => {
      saveSection('Mode')
    })
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('dns_resolution_mode', 'forward')
    })
  })

  it('saving an unchanged mode is a no-op', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByLabelText('Mode').value).toBe('auto')
    })
    await act(async () => {
      saveSection('Mode')
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expect(mockToast.info).toHaveBeenCalled()
  })
})

describe('SystemSettings local DNS forwarders', () => {
  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockGetLocales.mockClear()
    mockDnsStatus.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
    mockDnsStatus.mockImplementation(() =>
      Promise.resolve({
        enabled: true,
        running: true,
        tld: 'home',
        record_count: 0,
        local_forwarders: false,
        forwarders: ['8.8.8.8:53', '8.8.4.4:53'],
      }),
    )
    mockGetLocales.mockImplementation(() => Promise.resolve({
      current: 'en-US',
      populated: ['en-US'],
      common_languages: [{ code: 'en-US', native_name: 'English', english_name: 'English' }],
      extended_locales: [],
    }))
  })

  // The switch is a Radix Switch, which renders a button carrying aria-checked
  // rather than a checkbox input. Reading that attribute is what a screen reader
  // does, so it is also what tells us the control says what it means.
  function localForwardersSwitchState() {
    return screen.getByLabelText('Use local resolvers').getAttribute('aria-checked')
  }

  it('is off when the setting is absent', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(localForwardersSwitchState()).toBe('false')
    })
  })

  it('reflects the stored value', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, dns_local_forwarders: 'true' }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(localForwardersSwitchState()).toBe('true')
    })
  })

  it('turns the setting on', async () => {
    renderSystemSettings()
    await waitFor(() => {
      expect(localForwardersSwitchState()).toBe('false')
    })
    fireEvent.click(screen.getByLabelText('Use local resolvers'))
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('dns_local_forwarders', 'true')
    })
  })

  it('turns the setting back off', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, dns_local_forwarders: 'true' }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(localForwardersSwitchState()).toBe('true')
    })
    fireEvent.click(screen.getByLabelText('Use local resolvers'))
    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('dns_local_forwarders', 'false')
    })
  })

  // The addresses come from /dns/status, not from the setting: what rolodex
  // actually forwards to is the only thing that tells an operator the switch
  // took effect.
  it('shows the forwarders rolodex is configured with', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, dns_local_forwarders: 'true' }),
    )
    mockDnsStatus.mockImplementation(() =>
      Promise.resolve({
        enabled: true,
        running: true,
        tld: 'home',
        record_count: 0,
        local_forwarders: true,
        forwarders: ['192.168.7.1:53'],
      }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(screen.getByText('192.168.7.1:53')).toBeTruthy()
    })
  })

  // Discovery can find nothing usable — a box with no lease, or one whose only
  // nameserver line is a loopback stub. The switch then reads as on while the
  // public forwarders are still what rolodex holds, and saying so is the whole
  // point of reading the effective list back.
  it('says so when no local resolver was found', async () => {
    mockGetSettings.mockImplementation(() =>
      Promise.resolve({ ...defaultSettings, dns_local_forwarders: 'true' }),
    )
    mockDnsStatus.mockImplementation(() =>
      Promise.resolve({
        enabled: true,
        running: true,
        tld: 'home',
        record_count: 0,
        local_forwarders: true,
        forwarders: [],
      }),
    )
    renderSystemSettings()
    await waitFor(() => {
      expect(
        screen.getByText('No local resolver was found, so the public forwarders are still in use.'),
      ).toBeTruthy()
    })
  })
})

// The language dropdown against a page whose locale did NOT come from the
// server setting.
//
// This is the normal case, not an edge case: I18nContext picks the locale from
// localStorage or navigator.languages and pins it, and the server's global
// `locale` setting stays at its en-US default unless somebody deliberately
// changes it. The settings page therefore has two candidate "current"
// languages, and it used to show the wrong one — the dropdown opened on
// English while the page around it was in Spanish, and Save compared the choice
// against the server value, so pressing it on the English it was already
// displaying took the nothing-to-do path. English was unreachable without
// selecting a third language, saving, and coming back.
describe('SystemSettings language reflects the locale actually in use', () => {
  const localeCatalog = {
    current: 'en-US',
    populated: ['en-US', 'es-ES', 'de-DE', 'de-AT', 'en-GB'],
    common_languages: [
      { code: 'en-US', native_name: 'English', english_name: 'English' },
      { code: 'es-ES', native_name: 'Español', english_name: 'Spanish' },
      { code: 'de-DE', native_name: 'Deutsch', english_name: 'German' },
    ],
    extended_locales: [
      { code: 'de-AT', native_name: 'Deutsch (Österreich)', english_name: 'German (Austria)' },
      { code: 'en-GB', native_name: 'English (UK)', english_name: 'English (UK)' },
    ],
  }

  beforeEach(() => {
    mockGetSettings.mockClear()
    mockSetSetting.mockClear()
    mockGetLocales.mockClear()
    mockToast.info.mockClear()
    mockToast.success.mockClear()
    mockToast.error.mockClear()
    mockGetSettings.mockImplementation(() => Promise.resolve({ ...defaultSettings }))
    mockSetSetting.mockImplementation(() => Promise.resolve())
    mockGetLocales.mockImplementation(() => Promise.resolve({ ...localeCatalog }))
    // setLocale() records the choice for the browser; a leftover from one test
    // would decide the starting locale of the next one.
    localStorage.clear()
  })

  // Render inside a real I18nProvider pinned to `locale`, which is what a
  // browser-detected or previously-chosen language looks like to this page.
  function renderInLocale(locale) {
    return render(
      <MemoryRouter>
        <I18nProvider initialLocale={locale}>
          <SystemSettings />
        </I18nProvider>
      </MemoryRouter>,
    )
  }

  // Query by id, not by label: the labels are translated, and the point of
  // these tests is that the page is not in English.
  function languageSelect() {
    const el = document.getElementById('language-select')
    expect(el).toBeTruthy()
    return el
  }

  async function saveLanguage(catalog) {
    const form = languageSelect().closest('form')
    const button = within(form).getByRole('button', { name: catalog['settings.save_btn'] })
    await act(async () => {
      fireEvent.click(button)
    })
  }

  it('preselects the page language, not the server setting', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    // Sanity: the server really is saying something else, so the assertion
    // above cannot pass by both values agreeing.
    expect(localeCatalog.current).toBe('en-US')
  })

  it('leaves the dropdown on a real option rather than blank', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      const select = languageSelect()
      expect(select.value).toBe('es-ES')
      expect(select.selectedIndex).toBeGreaterThanOrEqual(0)
      expect(select.options[select.selectedIndex].value).toBe('es-ES')
    })
  })

  // The reported bug. English is what the dropdown shows on a Spanish page, and
  // it is also what the server has stored, so the old comparison against the
  // server value made the button inert.
  it('switches the page to the language shown when it matches the server setting', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(screen.getByText(esES['settings.title'])).toBeTruthy()
    })
    const user = userEvent.setup()
    await user.selectOptions(languageSelect(), 'en-US')
    await saveLanguage(esES)

    // The page is now in English...
    await waitFor(() => {
      expect(screen.getByText(enUS['settings.title'])).toBeTruthy()
    })
    // ...it did not claim there was nothing to do...
    expect(mockToast.info).not.toHaveBeenCalled()
    expect(mockToast.success).toHaveBeenCalled()
    // ...and it did not write a setting that already held that value.
    expect(mockSetSetting).not.toHaveBeenCalled()
  })

  // The workaround the user was forced into — pick a third language, save, pick
  // the wanted one, save — must not be what makes it work. Both saves in the
  // sequence have to land, against a server whose stored value moves with them.
  it('applies both saves when the user switches away and back', async () => {
    let serverCurrent = 'en-US'
    mockSetSetting.mockImplementation((key, value) => {
      if (key === 'locale') serverCurrent = value
      return Promise.resolve()
    })
    mockGetLocales.mockImplementation(() =>
      Promise.resolve({ ...localeCatalog, current: serverCurrent }),
    )
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    const user = userEvent.setup()

    await user.selectOptions(languageSelect(), 'de-DE')
    await saveLanguage(esES)
    await waitFor(() => {
      expect(screen.getByText(deDE['settings.title'])).toBeTruthy()
    })
    // Let the reload the save triggers settle before touching the select
    // again, so the second selection cannot be overwritten by it. The value
    // is unobservable here (it reloads to the same de-DE), so wait on the
    // refetch itself and then flush its resolution.
    await waitFor(() => {
      expect(mockGetLocales).toHaveBeenCalledTimes(2)
    })
    await act(async () => {})
    expect(languageSelect().value).toBe('de-DE')
    expect(serverCurrent).toBe('de-DE')

    // Back to Spanish. The page is German and the server says de-DE, so this
    // is a real change on both counts.
    await user.selectOptions(languageSelect(), 'es-ES')
    await saveLanguage(deDE)
    await waitFor(() => {
      expect(screen.getByText(esES['settings.title'])).toBeTruthy()
    })
    expect(serverCurrent).toBe('es-ES')
    expect(mockToast.info).not.toHaveBeenCalled()
  })

  it('persists and applies a language that differs from both page and server', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    const user = userEvent.setup()
    await user.selectOptions(languageSelect(), 'de-DE')
    await saveLanguage(esES)

    await waitFor(() => {
      expect(mockSetSetting).toHaveBeenCalledWith('locale', 'de-DE')
    })
    await waitFor(() => {
      expect(screen.getByText(deDE['settings.title'])).toBeTruthy()
    })
  })

  // The confirmation is read next to a UI that has already switched, so it is
  // written in the language switched *to*. `t()` closes over the render it was
  // called from, which is still the old language — the one message on the page
  // whose subject is that that language is no longer in use.
  it('announces the change in the language switched to', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    const user = userEvent.setup()
    await user.selectOptions(languageSelect(), 'de-DE')
    await saveLanguage(esES)

    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith(deDE['settings.toast_language_updated'])
    })
    expect(mockToast.success).not.toHaveBeenCalledWith(esES['settings.toast_language_updated'])
  })

  // Same rule on the path that writes no setting: the announcement follows the
  // page, not the server.
  it('announces in the target language even when only the page changes', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    const user = userEvent.setup()
    await user.selectOptions(languageSelect(), 'en-US')
    await saveLanguage(esES)

    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith(enUS['settings.toast_language_updated'])
    })
    expect(mockSetSetting).not.toHaveBeenCalled()
    expect(mockToast.success).not.toHaveBeenCalledWith(esES['settings.toast_language_updated'])
  })

  it('announces in the target language when leaving English', async () => {
    renderInLocale('en-US')
    await waitFor(() => {
      expect(languageSelect().value).toBe('en-US')
    })
    const user = userEvent.setup()
    await user.selectOptions(languageSelect(), 'es-ES')
    await saveLanguage(enUS)

    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith(esES['settings.toast_language_updated'])
    })
    expect(mockToast.success).not.toHaveBeenCalledWith(enUS['settings.toast_language_updated'])
  })

  // A country locale inherits its base catalog, so the announcement has to
  // arrive in inherited German rather than dropping to English.
  it('announces a derived country locale in its inherited language', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    const user = userEvent.setup()
    // de-AT is in the extended list, which the toggle reveals.
    const form = languageSelect().closest('form')
    const toggle = within(form)
      .getAllByRole('button')
      .find((b) => b.getAttribute('type') === 'button')
    await act(async () => {
      fireEvent.click(toggle)
    })
    await user.selectOptions(languageSelect(), 'de-AT')
    await saveLanguage(esES)

    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith(deAT['settings.toast_language_updated'])
    })
    expect(mockToast.success).not.toHaveBeenCalledWith(enUS['settings.toast_language_updated'])
  })

  it('remembers the choice for this browser', async () => {
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    const user = userEvent.setup()
    await user.selectOptions(languageSelect(), 'en-US')
    await saveLanguage(esES)
    await waitFor(() => {
      expect(localStorage.getItem('townos.locale')).toBe('en-US')
    })
  })

  // Saving the language the page is already in, when the server agrees too,
  // really is nothing to do — the no-op path must survive the fix.
  it('still reports nothing to do when page and server both match the choice', async () => {
    mockGetLocales.mockImplementation(() =>
      Promise.resolve({ ...localeCatalog, current: 'es-ES' }),
    )
    renderInLocale('es-ES')
    await waitFor(() => {
      expect(languageSelect().value).toBe('es-ES')
    })
    await saveLanguage(esES)
    expect(mockSetSetting).not.toHaveBeenCalled()
    expect(mockToast.info).toHaveBeenCalledWith(esES['settings.toast_nothing_to_do'])
  })

  // A country locale lives in the collapsed extended list. Preselecting it
  // without expanding that list would set the select to a value none of its
  // rendered options carry, which renders as an empty box — the same "shows
  // the wrong language" symptom in a different disguise.
  it('expands the extended list when the page language lives there', async () => {
    renderInLocale('de-AT')
    await waitFor(() => {
      expect(languageSelect().value).toBe('de-AT')
    })
    const select = languageSelect()
    const codes = Array.from(select.options).map((o) => o.value)
    expect(codes).toContain('de-AT')
    // The whole extended list is showing, not just the selected entry.
    expect(codes).toContain('en-GB')
    expect(select.options[select.selectedIndex].value).toBe('de-AT')
  })

  // Collapsing the list again must not erase the language in use from the
  // control — that is the empty-box failure arriving by the other door.
  it('keeps an extended selection visible when the list is collapsed', async () => {
    renderInLocale('de-AT')
    await waitFor(() => {
      expect(languageSelect().value).toBe('de-AT')
    })
    const form = languageSelect().closest('form')
    const toggle = within(form)
      .getAllByRole('button')
      .find((b) => b.getAttribute('type') === 'button')
    expect(toggle).toBeTruthy()
    await act(async () => {
      fireEvent.click(toggle)
    })
    const select = languageSelect()
    expect(select.value).toBe('de-AT')
    expect(select.options[select.selectedIndex].value).toBe('de-AT')
    // The rest of the extended list really is gone, so this is the collapsed
    // state and not a toggle that failed to fire.
    const codes = Array.from(select.options).map((o) => o.value)
    expect(codes).toEqual(['en-US', 'es-ES', 'de-DE', 'de-AT'])
  })

  it('falls back to the server setting when the page language is not offered', async () => {
    // A catalog the server does not advertise (nothing lists fr-FR here).
    renderInLocale('fr-FR')
    await waitFor(() => {
      expect(languageSelect().value).toBe('en-US')
    })
  })

  it('saving from an unlisted page language still applies the choice', async () => {
    renderInLocale('fr-FR')
    await waitFor(() => {
      expect(languageSelect().value).toBe('en-US')
    })
    // en-US is the server's value, so only the page needs to change.
    const form = languageSelect().closest('form')
    const button = within(form).getByRole('button', { name: frFR['settings.save_btn'] })
    await act(async () => {
      fireEvent.click(button)
    })
    await waitFor(() => {
      expect(screen.getByText(enUS['settings.title'])).toBeTruthy()
    })
    expect(mockToast.info).not.toHaveBeenCalled()
  })
})
