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
  const onClose = overrides.onClose || vi.fn()
  const dialog = {
    open: true,
    name: 'nginx',
    version: '1.0',
    questions: { hostname: { query: 'What hostname?' } },
    responses: {},
    fieldErrors: {},
    clearedFields: {},
    ...(overrides.dialog || {}),
  }
  render(
    <TooltipProvider>
      <InstallQuestionsDialog
        dialog={dialog}
        onClose={onClose}
        onSubmit={onSubmit}
        onClearField={() => {}}
      />
    </TooltipProvider>,
  )
  return { onSubmit, onClose }
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
      submittedNetwork = e.currentTarget.elements['__network__'].value
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
      submittedNetwork = e.currentTarget.elements['__network__'].value
    })
    renderDialog({ onSubmit })

    const select = await screen.findByRole('combobox')
    fireEvent.submit(select.closest('form'))

    expect(submittedNetwork).toBe('home')
  })
})

describe('InstallQuestionsDialog boolean questions', () => {
  // The form is read by name, so a boolean must submit "true"/"false" — never a
  // checkbox's native "on", and never nothing at all when it is unchecked.
  function renderBoolean({ question = {}, responses = {} } = {}) {
    let submitted = null
    const onSubmit = vi.fn((e) => {
      e.preventDefault()
      submitted = e.currentTarget.elements['open'].value
    })
    renderDialog({
      onSubmit,
      dialog: {
        questions: { open: { query: 'Allow open registration?', type: 'boolean', ...question } },
        responses,
      },
    })
    return {
      submit: async () => {
        const box = await screen.findByRole('checkbox', { name: 'Allow open registration?' })
        fireEvent.submit(box.closest('form'))
        return submitted
      },
      checkbox: () => screen.getByRole('checkbox', { name: 'Allow open registration?' }),
    }
  }

  it('renders a checkbox instead of a text input', async () => {
    renderBoolean()
    const box = await screen.findByRole('checkbox', { name: 'Allow open registration?' })
    expect(box.checked).toBe(false)
    // No free-text field for this question.
    expect(screen.queryByRole('textbox', { name: 'Allow open registration?' })).toBeNull()
  })

  it('submits "false" when the box is left unchecked', async () => {
    const { submit } = renderBoolean()
    expect(await submit()).toBe('false')
  })

  it('submits "true" once the box is checked', async () => {
    const { submit, checkbox } = renderBoolean()
    await screen.findByRole('checkbox', { name: 'Allow open registration?' })
    fireEvent.click(checkbox())
    expect(await submit()).toBe('true')
  })

  it('starts checked when the package default is true', async () => {
    const { submit, checkbox } = renderBoolean({ question: { default: 'true' } })
    await screen.findByRole('checkbox', { name: 'Allow open registration?' })
    expect(checkbox().checked).toBe(true)
    expect(await submit()).toBe('true')
  })

  it('lets the user turn off a default-on option', async () => {
    const { submit, checkbox } = renderBoolean({ question: { default: 'true' } })
    await screen.findByRole('checkbox', { name: 'Allow open registration?' })
    fireEvent.click(checkbox())
    expect(await submit()).toBe('false')
  })

  it('reflects a cached response rather than a read-only value with a clear button', async () => {
    const { submit, checkbox } = renderBoolean({
      question: { default: 'true' },
      responses: { open: 'false' },
    })
    await screen.findByRole('checkbox', { name: 'Allow open registration?' })
    // The saved answer wins over the default, and stays directly editable.
    expect(checkbox().checked).toBe(false)
    expect(await submit()).toBe('false')
  })
})

describe('InstallQuestionsDialog cached secrets', () => {
  function renderSecret(overrides = {}) {
    const onClearField = vi.fn()
    render(
      <TooltipProvider>
        <InstallQuestionsDialog
          dialog={{
            open: true,
            name: 'redis',
            version: '1.0',
            questions: { pass: { query: 'Enter password', type: 'secret' } },
            responses: { pass: 'my-secret' },
            fieldErrors: {},
            clearedFields: {},
            ...overrides,
          }}
          onClose={vi.fn()}
          onSubmit={(e) => e.preventDefault()}
          onClearField={onClearField}
        />
      </TooltipProvider>,
    )
    return { onClearField }
  }

  it('shows the cached secret in the clear rather than masked', async () => {
    renderSecret()
    expect(await screen.findByText('my-secret')).toBeTruthy()
    expect(screen.queryByText('********')).toBeNull()
  })

  it('offers a recycle button, not a clear X, and reports the tooltip on hover', async () => {
    const { onClearField } = renderSecret()
    const btn = await screen.findByRole('button', { name: 'Replace this secret' })
    expect(btn.querySelector('svg.lucide-rotate-cw')).toBeTruthy()
    expect(btn.querySelector('svg.lucide-x')).toBeNull()

    fireEvent.focus(btn)
    await waitFor(() => {
      expect(
        screen.getAllByText('Replace this secret — leave the new field empty to generate one').length,
      ).toBeGreaterThan(0)
    })

    fireEvent.click(btn)
    expect(onClearField).toHaveBeenCalledWith('pass')
  })

  it('keeps the clear X for non-secret cached answers', async () => {
    const { onClearField } = renderSecret({
      questions: { port: { query: 'Port?', type: 'port' } },
      responses: { port: '8080' },
    })
    const btn = await screen.findByRole('button', { name: 'Clear this value' })
    expect(btn.querySelector('svg.lucide-x')).toBeTruthy()

    fireEvent.focus(btn)
    await waitFor(() => {
      expect(screen.getAllByText('Clear to enter a new value').length).toBeGreaterThan(0)
    })

    fireEvent.click(btn)
    expect(onClearField).toHaveBeenCalledWith('port')
  })

  it('renders a recycled secret as a visible text input that auto-generates when left empty', async () => {
    renderSecret({ clearedFields: { pass: true } })
    const input = await screen.findByLabelText('Enter password')
    // The operator asked to see what they type; a password input hides it.
    expect(input.type).toBe('text')
    expect(input.value).toBe('')
    expect(input.placeholder).toBe('Auto-generated if empty')
  })
})

describe('InstallQuestionsDialog dismissal', () => {
  it('stays open when the user clicks outside it', async () => {
    const { onClose } = renderDialog()
    await screen.findByRole('combobox')

    fireEvent.pointerDown(document.body)

    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeTruthy()
  })

  it('closes when the cancel button is pressed', async () => {
    const { onClose } = renderDialog()
    await screen.findByRole('combobox')

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(onClose).toHaveBeenCalled()
  })
})
