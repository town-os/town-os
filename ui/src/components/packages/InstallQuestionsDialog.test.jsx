import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { TooltipProvider } from '@/components/ui/tooltip'
import InstallQuestionsDialog from './InstallQuestionsDialog.jsx'

const mockClient = {
  startOAuth: vi.fn(),
  pollOAuth: vi.fn(),
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

// An oauth question is answered by running a device flow, not by typing: the user
// clicks Connect, approves at the provider, and the token the flow returns
// becomes the form's value. This is what replaces running a shell script by hand
// to obtain a Plex token.
describe('InstallQuestionsDialog oauth questions', () => {
  const oauthDialog = {
    repo: 'default',
    name: 'plex',
    version: '3.0',
    questions: { plextoken: { query: 'Plex account', type: 'oauth' } },
  }

  beforeEach(() => {
    mockClient.startOAuth.mockReset()
    mockClient.pollOAuth.mockReset()
    vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  it('renders a Connect button rather than a text field', () => {
    renderDialog({ dialog: oauthDialog })

    expect(screen.getByRole('button', { name: 'Connect' })).toBeTruthy()
    // A token is not something anyone types; there must be no text input to type
    // it into.
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('opens the approval page and submits the token the flow returns', async () => {
    mockClient.startOAuth.mockResolvedValue({
      flow_id: 'flow-1',
      approve_url: 'https://app.plex.tv/auth#?code=abcd',
      interval_ms: 1000,
    })
    // Pending first: the operator has not approved yet, which is the normal
    // state for the first poll or several.
    mockClient.pollOAuth
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ status: 'approved', token: 'plex-auth-token' })

    const onSubmit = vi.fn((e) => e.preventDefault())
    renderDialog({ dialog: oauthDialog, onSubmit })

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => expect(window.open).toHaveBeenCalledWith(
      'https://app.plex.tv/auth#?code=abcd',
      '_blank',
      'noopener,noreferrer',
    ))
    await waitFor(() => expect(screen.getByText('Connected')).toBeTruthy(), { timeout: 5000 })

    // The token is the form's value for the question, exactly as if it had been
    // typed into a text field.
    // The dialog renders through a portal, so the field lives on document, not in
    // render()'s container.
    const field = document.querySelector('input[name="plextoken"]')
    expect(field.value).toBe('plex-auth-token')
    expect(mockClient.startOAuth).toHaveBeenCalledWith('default', 'plex', '3.0', 'plextoken')
  }, 10000)

  it('shows the user code when the provider requires one', async () => {
    mockClient.startOAuth.mockResolvedValue({
      flow_id: 'flow-2',
      approve_url: 'https://github.com/login/device',
      user_code: 'WXYZ-1234',
      interval_ms: 1000,
    })
    mockClient.pollOAuth.mockResolvedValue({ status: 'pending' })

    renderDialog({ dialog: oauthDialog })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => expect(screen.getByText('WXYZ-1234')).toBeTruthy())
  })

  it('reports an expired approval request instead of polling forever', async () => {
    mockClient.startOAuth.mockResolvedValue({
      flow_id: 'flow-3',
      approve_url: 'https://app.plex.tv/auth',
      interval_ms: 1000,
    })
    mockClient.pollOAuth.mockResolvedValue({ status: 'expired' })

    renderDialog({ dialog: oauthDialog })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(
      () => expect(screen.getByText('The approval request expired. Connect again.')).toBeTruthy(),
      { timeout: 5000 },
    )
  }, 10000)

  // Reinstalling must not force the operator back through the provider: the token
  // from the previous install is already a valid answer.
  it('treats a cached token as already connected', () => {
    renderDialog({
      dialog: { ...oauthDialog, responses: { plextoken: 'saved-token' } },
    })

    expect(screen.getByText('Connected')).toBeTruthy()
    expect(document.querySelector('input[name="plextoken"]').value).toBe('saved-token')
    expect(screen.getByRole('button', { name: 'Reconnect' })).toBeTruthy()
  })
})

// A failed reconnect must not be reported as a success. The cached token from the
// previous install is still the form's value -- it is still a valid answer -- but
// a green "Connected" beside a red error tells the operator the attempt they are
// looking at worked, which it did not.
describe('InstallQuestionsDialog oauth errors', () => {
  const oauthDialog = {
    repo: 'default',
    name: 'plex',
    version: '3.0',
    questions: { plextoken: { query: 'Plex account', type: 'oauth' } },
  }

  beforeEach(() => {
    mockClient.startOAuth.mockReset()
    mockClient.pollOAuth.mockReset()
    vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  it('drops the Connected badge when a reconnect fails, but keeps the cached token', async () => {
    mockClient.startOAuth.mockRejectedValue({ detail: 'oauth URL not allowed' })

    renderDialog({
      dialog: { ...oauthDialog, responses: { plextoken: 'saved-token' } },
    })

    expect(screen.getByText('Connected')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))

    await waitFor(() => expect(screen.getByText('oauth URL not allowed')).toBeTruthy())
    expect(screen.queryByText('Connected')).toBeNull()
    // The answer is still there to install with; only the claim of success is gone.
    expect(document.querySelector('input[name="plextoken"]').value).toBe('saved-token')
    expect(screen.getByRole('button', { name: 'Reconnect' })).toBeTruthy()
  })

  it('does not claim Connected when the flow expires', async () => {
    mockClient.startOAuth.mockResolvedValue({
      flow_id: 'flow-x',
      approve_url: 'https://app.plex.tv/auth',
      interval_ms: 1000,
    })
    mockClient.pollOAuth.mockResolvedValue({ status: 'expired' })

    renderDialog({
      dialog: { ...oauthDialog, responses: { plextoken: 'saved-token' } },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))

    await waitFor(
      () => expect(screen.getByText('The approval request expired. Connect again.')).toBeTruthy(),
      { timeout: 5000 },
    )
    expect(screen.queryByText('Connected')).toBeNull()
  }, 10000)

  it('reports a failed start with no cached token and offers Connect again', async () => {
    mockClient.startOAuth.mockRejectedValue(new Error('provider returned 500'))

    renderDialog({ dialog: oauthDialog })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => expect(screen.getByText('provider returned 500')).toBeTruthy())
    expect(screen.queryByText('Connected')).toBeNull()
    expect(document.querySelector('input[name="plextoken"]').value).toBe('')
  })
})

// The two states that used to have no answer at all, because "connected" was
// derived from holding a token rather than from what the flow was doing.
describe('InstallQuestionsDialog oauth in-flight and empty-token states', () => {
  const oauthDialog = {
    repo: 'default',
    name: 'plex',
    version: '3.0',
    questions: { plextoken: { query: 'Plex account', type: 'oauth' } },
    responses: { plextoken: 'saved-token' },
  }

  beforeEach(() => {
    mockClient.startOAuth.mockReset()
    mockClient.pollOAuth.mockReset()
    vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  // A reconnect that is still running has not connected to anything. The cached
  // token is still the answer, but the badge describes the attempt, not the value.
  it('drops the Connected badge while a reconnect is in flight', async () => {
    let releaseStart
    mockClient.startOAuth.mockReturnValue(new Promise((resolve) => { releaseStart = resolve }))

    renderDialog({ dialog: oauthDialog })
    expect(screen.getByText('Connected')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))

    await waitFor(() => expect(screen.getByText('Starting…')).toBeTruthy())
    expect(screen.queryByText('Connected')).toBeNull()
    // Still installable with what was already held.
    expect(document.querySelector('input[name="plextoken"]').value).toBe('saved-token')

    releaseStart({ flow_id: 'f', approve_url: 'https://app.plex.tv/auth', interval_ms: 1000 })
    await waitFor(() => expect(screen.getByText('Waiting for approval…')).toBeTruthy())
    expect(screen.queryByText('Connected')).toBeNull()
  })

  // "Approved" with nothing in hand is a failed flow. Reporting it as success
  // would install the package with an empty credential.
  it('treats an approval that carries no token as an error', async () => {
    mockClient.startOAuth.mockResolvedValue({
      flow_id: 'f',
      approve_url: 'https://app.plex.tv/auth',
      interval_ms: 1000,
    })
    mockClient.pollOAuth.mockResolvedValue({ status: 'approved', token: '' })

    renderDialog({ dialog: { ...oauthDialog, responses: {} } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(
      () => expect(
        screen.getByText('The provider approved the request but returned no token.'),
      ).toBeTruthy(),
      { timeout: 5000 },
    )
    expect(screen.queryByText('Connected')).toBeNull()
    expect(document.querySelector('input[name="plextoken"]').value).toBe('')
  }, 10000)
})
