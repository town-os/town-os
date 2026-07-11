import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import VolumeReuseDialog from './VolumeReuseDialog.jsx'

function renderDialog() {
  const onClose = vi.fn()
  const dialog = {
    open: true,
    repo: 'default',
    name: 'nginx',
    version: '1.0',
    uninstalledVersions: ['0.9'],
  }
  render(
    <VolumeReuseDialog
      dialog={dialog}
      onClose={onClose}
      onStartFresh={vi.fn()}
      onReuse={vi.fn()}
    />,
  )
  return { onClose }
}

describe('VolumeReuseDialog dismissal', () => {
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
