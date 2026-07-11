import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import InstallPreviewDialog from './InstallPreviewDialog.jsx'

function renderDialog() {
  const onClose = vi.fn()
  const onContinue = vi.fn()
  const dialog = {
    open: true,
    repo: 'default',
    name: 'nginx',
    version: '1.0',
    description: 'A web server',
    image: 'docker.io/library/nginx:latest',
    runtime: 'container',
    volumes: [],
    ports: [],
  }
  render(
    <InstallPreviewDialog dialog={dialog} onClose={onClose} onContinue={onContinue} />,
  )
  return { onClose, onContinue }
}

describe('InstallPreviewDialog dismissal', () => {
  it('stays open when the user clicks outside it', () => {
    const { onClose } = renderDialog()

    fireEvent.pointerDown(document.body)

    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeTruthy()
  })

  it('closes when the cancel button is pressed', () => {
    const { onClose } = renderDialog()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(onClose).toHaveBeenCalled()
  })
})
