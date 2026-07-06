import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { TooltipProvider } from '@/components/ui/tooltip'
import InstallQuestionsDialog from './InstallQuestionsDialog.jsx'

const mockClient = {
  listNetworks: vi.fn(() =>
    Promise.resolve([
      { name: 'home', enabled: true },
      { name: 'office', enabled: true },
      { name: 'lab', enabled: false }, // disabled → must be excluded from the picker
    ]),
  ),
}

vi.mock('@/lib/client-instance.js', () => ({
  default: () => mockClient,
}))

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
})

function renderDialog(overrides = {}) {
  const onSubmit = overrides.onSubmit || vi.fn((e) => e.preventDefault())
  const dialog = {
    open: true,
    name: 'nginx',
    version: '1.0',
    questions: { hostname: { query: 'What hostname?' } },
    responses: {},
    fieldErrors: {},
    clearedFields: {},
  }
  render(
    <TooltipProvider>
      <InstallQuestionsDialog
        dialog={dialog}
        onClose={() => {}}
        onSubmit={onSubmit}
        onClearField={() => {}}
      />
    </TooltipProvider>,
  )
  return { onSubmit }
}

describe('InstallQuestionsDialog network selector', () => {
  beforeEach(() => {
    mockClient.listNetworks.mockClear()
  })

  it('populates the selector from listNetworks with enabled networks only', async () => {
    renderDialog()
    await waitFor(() => expect(mockClient.listNetworks).toHaveBeenCalled())

    // The office (enabled) option must appear once networks load.
    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'office' })).toBeTruthy()
    })

    const select = screen.getByRole('combobox')
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.value)
    expect(values).toContain('home')
    expect(values).toContain('office')
    // Disabled networks are filtered out of the install picker.
    expect(values).not.toContain('lab')
  })

  it('submits the selected network on the form', async () => {
    let submittedNetwork = null
    const onSubmit = vi.fn((e) => {
      e.preventDefault()
      submittedNetwork = e.currentTarget['__network__'].value
    })
    renderDialog({ onSubmit })

    // Wait for the enabled options to load, then pick "office".
    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'office' })).toBeTruthy()
    })
    const select = screen.getByRole('combobox')
    fireEvent.change(select, { target: { value: 'office' } })

    fireEvent.submit(select.closest('form'))

    expect(onSubmit).toHaveBeenCalled()
    expect(submittedNetwork).toBe('office')
  })

  it('defaults to home before any network is chosen', async () => {
    let submittedNetwork = null
    const onSubmit = vi.fn((e) => {
      e.preventDefault()
      submittedNetwork = e.currentTarget['__network__'].value
    })
    renderDialog({ onSubmit })

    const select = await screen.findByRole('combobox')
    fireEvent.submit(select.closest('form'))

    expect(submittedNetwork).toBe('home')
  })
})
