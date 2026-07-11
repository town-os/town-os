import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import PackageInfoDialog from './PackageInfoDialog.jsx'

function renderInfo(dialog) {
  render(
    <PackageInfoDialog
      dialog={{ open: true, name: 'synapse', version: '1.0', ...dialog }}
      onClose={vi.fn()}
    />,
  )
}

describe('PackageInfoDialog boolean answers', () => {
  // Boolean answers are stored as the strings "true"/"false"; showing those raw
  // in the configuration list reads like a bug next to a checkbox in the
  // install dialog.
  it('renders a true answer as Yes', () => {
    renderInfo({
      questions: { open: { query: 'Allow open registration?', type: 'boolean' } },
      responses: { open: 'true' },
    })
    expect(screen.getByText('Yes')).toBeTruthy()
    expect(screen.queryByText('true')).toBeNull()
  })

  it('renders a false answer as No', () => {
    renderInfo({
      questions: { open: { query: 'Allow open registration?', type: 'boolean' } },
      responses: { open: 'false' },
    })
    expect(screen.getByText('No')).toBeTruthy()
    expect(screen.queryByText('false')).toBeNull()
  })

  it('renders an unanswered boolean as a dash', () => {
    renderInfo({
      questions: { open: { query: 'Allow open registration?', type: 'boolean' } },
      responses: {},
    })
    expect(screen.getByText('-')).toBeTruthy()
  })

  it('leaves non-boolean answers untouched', () => {
    renderInfo({
      questions: {
        port: { query: 'Port?', type: 'port' },
        pass: { query: 'Password?', type: 'secret' },
      },
      responses: { port: '8080', pass: 'hunter2' },
    })
    expect(screen.getByText('8080')).toBeTruthy()
    // Secrets stay masked.
    expect(screen.getByText('********')).toBeTruthy()
    expect(screen.queryByText('hunter2')).toBeNull()
  })
})
