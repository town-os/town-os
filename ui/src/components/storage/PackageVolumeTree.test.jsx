import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import PackageVolumeTree from './PackageVolumeTree.jsx'

function makeGroup({ pkg = 'nginx', effective = 'nginx', repo = 'core', versions } = {}) {
  const volumes = []
  for (const [version, names] of Object.entries(versions)) {
    for (const name of names) {
      volumes.push({
        name: `${version}/${name}`,
        internal_name: `installed/${repo}/${effective.replaceAll('--dep--', '/subpackages/')}/${version}/${name}`,
        repo,
        quota: 0,
        state: 'installed',
      })
    }
  }
  return { package: pkg, effective_name: effective, repo, volumes }
}

function renderTree(overrides = {}) {
  const props = {
    packageGroups: [],
    onModifyVolume: vi.fn(),
    onDownloadVolume: vi.fn(),
    onUploadVolume: vi.fn(),
    onDeleteVolume: vi.fn(),
    onDeletePackage: vi.fn(),
    onDeleteVersion: vi.fn(),
    ...overrides,
  }
  return { props, ...render(<PackageVolumeTree {...props} />) }
}

describe('PackageVolumeTree', () => {
  it('renders nothing when there are no groups', () => {
    const { container } = renderTree()
    expect(container.firstChild).toBeNull()
  })

  it('shows the package row with counts and no drill-in actions', () => {
    const group = makeGroup({ versions: { '1.0': ['data', 'config'], '2.0': ['data'] } })
    renderTree({ packageGroups: [group] })
    expect(screen.getByText('nginx')).toBeTruthy()
    // 2 versions, 3 volumes count text
    expect(screen.getByText(/2 versions, 3 volumes/)).toBeTruthy()
    // No Modify/Upload/Download on a collapsed tree
    expect(screen.queryByRole('button', { name: /Modify/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Upload archive' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Download archive' })).toBeNull()
  })

  it('expanding a package reveals version rows with their own delete buttons and no other actions', () => {
    const group = makeGroup({ versions: { '1.0': ['data'], '2.0': ['data'] } })
    renderTree({ packageGroups: [group] })

    fireEvent.click(screen.getByText('nginx'))
    expect(screen.getByText('Version 1.0')).toBeTruthy()
    expect(screen.getByText('Version 2.0')).toBeTruthy()

    // Still no leaf-only actions.
    expect(screen.queryByRole('button', { name: /Modify/ })).toBeNull()
    // Destructive buttons now: package + version 1.0 + version 2.0 = 3
    const destructive = screen.getAllByRole('button').filter((b) => b.classList.contains('text-destructive'))
    expect(destructive.length).toBe(3)
  })

  it('expanding a version reveals leaf rows with the full action set', () => {
    const group = makeGroup({ versions: { '1.0': ['data', 'config'] } })
    renderTree({ packageGroups: [group] })
    fireEvent.click(screen.getByText('nginx'))
    fireEvent.click(screen.getByText('Version 1.0'))
    // Leaf names use just the volume piece, not the version prefix.
    expect(screen.getByText('data')).toBeTruthy()
    expect(screen.getByText('config')).toBeTruthy()
    // Two leaves, each with Modify + Upload + Download.
    expect(screen.getAllByRole('button', { name: /Modify/ }).length).toBe(2)
    expect(screen.getAllByRole('button', { name: 'Upload archive' }).length).toBe(2)
    expect(screen.getAllByRole('button', { name: 'Download archive' }).length).toBe(2)
  })

  it('package-level delete fires onDeletePackage with the group', () => {
    const group = makeGroup({ versions: { '1.0': ['data'] } })
    const { props } = renderTree({ packageGroups: [group] })
    const destructive = screen.getAllByRole('button').filter((b) => b.classList.contains('text-destructive'))
    // One destructive button when only the package row is visible.
    expect(destructive.length).toBe(1)
    fireEvent.click(destructive[0])
    expect(props.onDeletePackage).toHaveBeenCalledTimes(1)
    expect(props.onDeletePackage).toHaveBeenCalledWith(group)
    expect(props.onDeleteVersion).not.toHaveBeenCalled()
    expect(props.onDeleteVolume).not.toHaveBeenCalled()
  })

  it('version-level delete fires onDeleteVersion with (group, version)', () => {
    const group = makeGroup({ versions: { '1.0': ['data'], '2.0': ['data'] } })
    const { props } = renderTree({ packageGroups: [group] })
    fireEvent.click(screen.getByText('nginx'))
    // Use the row for Version 2.0 and click its destructive button.
    const versionRow = screen.getByText('Version 2.0').closest('tr')
    expect(versionRow).toBeTruthy()
    const versionDeleteBtn = within(versionRow).getAllByRole('button').find((b) => b.classList.contains('text-destructive'))
    fireEvent.click(versionDeleteBtn)
    expect(props.onDeleteVersion).toHaveBeenCalledTimes(1)
    expect(props.onDeleteVersion).toHaveBeenCalledWith(group, '2.0')
    expect(props.onDeletePackage).not.toHaveBeenCalled()
    expect(props.onDeleteVolume).not.toHaveBeenCalled()
  })

  it('leaf delete fires onDeleteVolume with the internal name', () => {
    const group = makeGroup({ versions: { '1.0': ['data'] } })
    const { props } = renderTree({ packageGroups: [group] })
    fireEvent.click(screen.getByText('nginx'))
    fireEvent.click(screen.getByText('Version 1.0'))
    const leafRow = screen.getByText('data').closest('tr')
    expect(leafRow).toBeTruthy()
    const leafDeleteBtn = within(leafRow).getAllByRole('button').find((b) => b.classList.contains('text-destructive'))
    fireEvent.click(leafDeleteBtn)
    expect(props.onDeleteVolume).toHaveBeenCalledWith('installed/core/nginx/1.0/data')
    expect(props.onDeletePackage).not.toHaveBeenCalled()
    expect(props.onDeleteVersion).not.toHaveBeenCalled()
  })

  it('leaf modify / upload / download fire the matching callbacks with the volume', () => {
    const group = makeGroup({ versions: { '1.0': ['data'] } })
    const { props } = renderTree({ packageGroups: [group] })
    fireEvent.click(screen.getByText('nginx'))
    fireEvent.click(screen.getByText('Version 1.0'))
    const leafRow = screen.getByText('data').closest('tr')
    fireEvent.click(within(leafRow).getByRole('button', { name: /Modify/ }))
    fireEvent.click(within(leafRow).getByRole('button', { name: 'Upload archive' }))
    fireEvent.click(within(leafRow).getByRole('button', { name: 'Download archive' }))
    // The tree enriches leaves with a displayName; callbacks receive the
    // enriched shape, so assert on the core identifying fields.
    const expectedVolume = expect.objectContaining({
      internal_name: group.volumes[0].internal_name,
      name: group.volumes[0].name,
      repo: group.volumes[0].repo,
      state: group.volumes[0].state,
    })
    expect(props.onModifyVolume).toHaveBeenCalledWith(expectedVolume)
    expect(props.onUploadVolume).toHaveBeenCalledWith(expectedVolume)
    expect(props.onDownloadVolume).toHaveBeenCalledWith(expectedVolume)
  })
})
