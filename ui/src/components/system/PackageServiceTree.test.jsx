import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import PackageServiceTree from './PackageServiceTree.jsx'
import { I18nProvider } from '@/i18n/I18nContext.jsx'

// Fixture: gitea -> postgres dep tree. Mirrors the canonical shape the
// backend's /systemd/units-tree endpoint serves — enough structure to
// prove rendering and action-routing logic.
function giteaTree() {
  return [
    {
      Name: 'town-os-package--core-gitea-1.0.service',
      ActiveState: 'active',
      SubState: 'running',
      package_identifier: 'core/gitea@1.0',
      display_identifier: 'core/gitea@1.0',
      package_description: 'Gitea',
      nc_active: true,
      repo: 'core',
      name: 'gitea',
      version: '1.0',
      children: [
        {
          Name: 'town-os-package--core-gitea--dep--postgres-15.0.service',
          ActiveState: 'active',
          SubState: 'running',
          package_identifier: 'core/gitea--dep--postgres@15.0',
          display_identifier: 'core/gitea/postgres@15.0',
          package_description: 'Postgres',
          is_dependency: true,
          repo: 'core',
          name: 'gitea--dep--postgres',
          version: '15.0',
          children: [],
        },
      ],
    },
  ]
}

function renderTree(overrides = {}) {
  return render(
    <I18nProvider>
      <PackageServiceTree
        roots={giteaTree()}
        onCascadeAction={vi.fn()}
        onUnitAction={vi.fn()}
        onViewLogs={vi.fn()}
        onViewNetworkLogs={vi.fn()}
        actionInProgress={false}
        {...overrides}
      />
    </I18nProvider>,
  )
}

/**
 * Radix DropdownMenu needs pointerDown (jsdom doesn't fully handle
 * click-to-open). Index selects the nth trigger in document order.
 */
function openDropdown(container, index = 0) {
  const triggers = container.querySelectorAll('[data-slot="dropdown-menu-trigger"]')
  fireEvent.pointerDown(triggers[index], { button: 0, pointerType: 'mouse' })
  fireEvent.click(triggers[index])
}

describe('PackageServiceTree', () => {
  beforeEach(() => {
    cleanup()
  })

  it('renders empty state when there are no roots', () => {
    render(
      <I18nProvider>
        <PackageServiceTree roots={[]} onCascadeAction={vi.fn()} onUnitAction={vi.fn()} onViewLogs={vi.fn()} onViewNetworkLogs={vi.fn()} />
      </I18nProvider>,
    )
    expect(screen.getByText(/no package services installed/i)).toBeTruthy()
  })

  it('renders root and expanded dep with pretty display identifier', () => {
    renderTree()
    expect(screen.getByText('core/gitea@1.0')).toBeTruthy()
    // Dep renders as pretty form "core/gitea/postgres@15.0".
    expect(screen.getByText('core/gitea/postgres@15.0')).toBeTruthy()
  })

  it('shows active status badge for running services', () => {
    renderTree()
    // Two "active" badges — one per unit in the tree.
    const activeBadges = screen.getAllByText('active')
    expect(activeBadges.length).toBeGreaterThanOrEqual(2)
  })

  it('shows failed (NC) label when nc_failed is set', () => {
    const trees = giteaTree()
    trees[0].nc_failed = true
    trees[0].ActiveState = 'failed'
    render(
      <I18nProvider>
        <PackageServiceTree
          roots={trees}
          onCascadeAction={vi.fn()}
          onUnitAction={vi.fn()}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    expect(screen.getByText(/failed \(NC\)/i)).toBeTruthy()
  })

  it('collapses children when root chevron is clicked', () => {
    const { container } = renderTree()
    // Dep is expanded by default.
    expect(screen.queryByText('core/gitea/postgres@15.0')).toBeTruthy()
    // Click root row (has the chevron) to collapse.
    const rootRow = container.querySelector('[data-testid="service-tree-row-core/gitea@1.0"]')
    fireEvent.click(rootRow)
    expect(screen.queryByText('core/gitea/postgres@15.0')).toBeFalsy()
  })

  it('dispatches cascade action for start on root row', async () => {
    const onCascade = vi.fn()
    const onUnit = vi.fn()
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={giteaTree()}
          onCascadeAction={onCascade}
          onUnitAction={onUnit}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    openDropdown(container, 0) // root dropdown
    const startItem = await screen.findByText('Start')
    fireEvent.click(startItem)

    expect(onCascade).toHaveBeenCalledTimes(1)
    expect(onCascade.mock.calls[0][0].package_identifier).toBe('core/gitea@1.0')
    expect(onCascade.mock.calls[0][1]).toBe('start')
    expect(onUnit).not.toHaveBeenCalled()
  })

  it('dispatches cascade action for stop on root row', async () => {
    const onCascade = vi.fn()
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={giteaTree()}
          onCascadeAction={onCascade}
          onUnitAction={vi.fn()}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    openDropdown(container, 0)
    const stopItem = await screen.findByText('Stop')
    fireEvent.click(stopItem)

    expect(onCascade).toHaveBeenCalledWith(
      expect.objectContaining({ package_identifier: 'core/gitea@1.0' }),
      'stop',
    )
  })

  it('dispatches cascade action for restart on root row', async () => {
    const onCascade = vi.fn()
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={giteaTree()}
          onCascadeAction={onCascade}
          onUnitAction={vi.fn()}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    openDropdown(container, 0)
    const restartItem = await screen.findByText('Restart')
    fireEvent.click(restartItem)

    expect(onCascade).toHaveBeenCalledWith(
      expect.objectContaining({ package_identifier: 'core/gitea@1.0' }),
      'restart',
    )
  })

  it('routes dep-row actions to onUnitAction, not onCascadeAction', async () => {
    const onCascade = vi.fn()
    const onUnit = vi.fn()
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={giteaTree()}
          onCascadeAction={onCascade}
          onUnitAction={onUnit}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    openDropdown(container, 1) // dep row's dropdown
    const restartItem = await screen.findByText('Restart')
    fireEvent.click(restartItem)

    expect(onUnit).toHaveBeenCalledWith(
      expect.objectContaining({ package_identifier: 'core/gitea--dep--postgres@15.0' }),
      'restart',
    )
    expect(onCascade).not.toHaveBeenCalled()
  })

  it('calls onViewLogs with the unit name', async () => {
    const onView = vi.fn()
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={giteaTree()}
          onCascadeAction={vi.fn()}
          onUnitAction={vi.fn()}
          onViewLogs={onView}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    openDropdown(container, 0)
    const logsItem = await screen.findByText('Service Logs')
    fireEvent.click(logsItem)

    expect(onView).toHaveBeenCalledTimes(1)
    expect(onView.mock.calls[0][0].Name).toBe('town-os-package--core-gitea-1.0.service')
  })

  it('shows network logs option only when nc_active', async () => {
    const { container } = renderTree()
    openDropdown(container, 0) // root, nc_active=true
    expect(await screen.findByText('Network Logs')).toBeTruthy()
  })

  it('hides network logs option when nc_active is false', async () => {
    const trees = giteaTree()
    trees[0].nc_active = false
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={trees}
          onCascadeAction={vi.fn()}
          onUnitAction={vi.fn()}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    openDropdown(container, 0)
    await screen.findByText('Service Logs')
    expect(screen.queryByText('Network Logs')).toBeFalsy()
  })

  it('hides Stop action for the systemcontroller unit', async () => {
    const trees = [
      {
        Name: 'town-os-systemcontroller.service',
        ActiveState: 'active',
        package_identifier: 'systemcontroller',
        display_identifier: 'systemcontroller',
        repo: 'core',
        name: 'systemcontroller',
        version: '1.0',
        children: [],
      },
    ]
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={trees}
          onCascadeAction={vi.fn()}
          onUnitAction={vi.fn()}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    openDropdown(container, 0)
    await screen.findByText('Start')
    expect(screen.queryByText('Stop')).toBeFalsy()
    // Restart is always available so operators can still recycle it.
    expect(screen.getByText('Restart')).toBeTruthy()
  })

  it('disables action items while an action is in progress', async () => {
    const { container } = render(
      <I18nProvider>
        <PackageServiceTree
          roots={giteaTree()}
          onCascadeAction={vi.fn()}
          onUnitAction={vi.fn()}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
          actionInProgress={true}
        />
      </I18nProvider>,
    )
    openDropdown(container, 0)
    const startItem = await screen.findByText('Start')
    // Radix reflects disabled state via data-disabled on the item role.
    const itemEl = startItem.closest('[role="menuitem"]')
    expect(itemEl?.getAttribute('data-disabled')).not.toBeNull()
  })

  it('shows dep count indicator on the root row', () => {
    renderTree()
    expect(screen.getByText(/\(1 dep\)/)).toBeTruthy()
  })

  it('renders three-level dep chain and expands all by default', () => {
    const trees = [
      {
        Name: 'town-os-package--core-app-1.0.service',
        ActiveState: 'active',
        package_identifier: 'core/app@1.0',
        display_identifier: 'core/app@1.0',
        repo: 'core',
        name: 'app',
        version: '1.0',
        children: [
          {
            Name: 'town-os-package--core-app--dep--db-15.0.service',
            ActiveState: 'active',
            package_identifier: 'core/app--dep--db@15.0',
            display_identifier: 'core/app/db@15.0',
            repo: 'core',
            name: 'app--dep--db',
            version: '15.0',
            children: [
              {
                Name: 'town-os-package--core-app--dep--db--dep--backup-2.0.service',
                ActiveState: 'active',
                package_identifier: 'core/app--dep--db--dep--backup@2.0',
                display_identifier: 'core/app/db/backup@2.0',
                repo: 'core',
                name: 'app--dep--db--dep--backup',
                version: '2.0',
                children: [],
              },
            ],
          },
        ],
      },
    ]
    render(
      <I18nProvider>
        <PackageServiceTree
          roots={trees}
          onCascadeAction={vi.fn()}
          onUnitAction={vi.fn()}
          onViewLogs={vi.fn()}
          onViewNetworkLogs={vi.fn()}
        />
      </I18nProvider>,
    )
    // All three rows render simultaneously because each level defaults
    // to expanded.
    expect(screen.getByText('core/app@1.0')).toBeTruthy()
    expect(screen.getByText('core/app/db@15.0')).toBeTruthy()
    expect(screen.getByText('core/app/db/backup@2.0')).toBeTruthy()
  })
})
