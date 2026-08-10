import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'

import OverviewTab from './OverviewTab.jsx'

/**
 * What the overview says when a partition publishes nothing.
 *
 * "Object storage isn't working" was as far as the report ever got, and the
 * screen is why: a partition whose daemon was down and a partition that was up
 * with nothing configured produced the same sentence. They call for completely
 * different actions — go and look at the daemon, versus publish something — so
 * the empty state has to name which one it is.
 */
describe('OverviewTab', () => {
  const RUNNING = {
    network: 'home',
    tld: 'home',
    quota: 0,
    running: true,
    names: [{ view: 's3', fqdn: 's3.gfeh.home', port: 9000, http: true }],
  }

  it('lists the published addresses of a running partition', () => {
    render(<OverviewTab partition={RUNNING} />)

    expect(screen.getByText('s3.gfeh.home')).toBeTruthy()
  })

  // A daemon that is not answering is the thing to say. It is also the thing to
  // say is being retried: the box re-converges its partitions on a timer, so
  // the operator's next step is to wait or read the unit's logs — not to
  // conclude the feature is missing and reboot.
  it('says the daemon is not answering when the partition is stopped', () => {
    render(<OverviewTab partition={{ ...RUNNING, running: false, names: [] }} />)

    expect(
      screen.getByText(/daemon is not answering/i),
    ).toBeTruthy()
    expect(screen.getByText(/retries every few minutes/i)).toBeTruthy()
  })

  // ... and a running partition with nothing published is not reported as a
  // failure, because it is not one.
  it('reports an empty running partition as configuration, not failure', () => {
    render(<OverviewTab partition={{ ...RUNNING, names: [] }} />)

    expect(screen.getByText('This partition publishes no addresses.')).toBeTruthy()
    expect(screen.queryByText(/not answering/i)).toBeNull()
  })

  // The whole table already turns on whether the ingress fronts a view, and an
  // address somebody can click is the difference between being told the name
  // and being able to use it — including the `index` row, which is the
  // browsable page listing all of these.
  it('makes an ingress-fronted address followable', () => {
    render(<OverviewTab partition={RUNNING} />)

    const link = screen.getByRole('link', { name: 's3.gfeh.home' })
    expect(link.getAttribute('href')).toBe('https://s3.gfeh.home')
  })

  // https:// on an SMB address is a handshake that completes and then does
  // nothing, which is exactly the failure that reads as a broken service.
  it('does not make an SMB address followable', () => {
    render(
      <OverviewTab
        partition={{ ...RUNNING, names: [{ view: 'smb', fqdn: 'smb.gfeh.home', port: 4450, http: false }] }}
      />,
    )

    expect(screen.queryByRole('link', { name: 'smb.gfeh.home' })).toBeNull()
    expect(screen.getByText('smb.gfeh.home')).toBeTruthy()
    // SMB keeps its number: there it is the real host port, and dialling it is
    // the only way in.
    expect(screen.getByText('4450')).toBeTruthy()
  })

  // The port reported for an HTTP view is the container-side port the ingress
  // proxies to. Printing 9000 in a column headed "Port" beside "Ingress
  // (HTTPS)" invites somebody to dial s3.gfeh.home:9000 and conclude object
  // storage is broken.
  it('does not print the container-side backend port of an HTTP view', () => {
    const { container } = render(<OverviewTab partition={RUNNING} />)

    expect(container.textContent).not.toContain('9000')
  })

  it('renders nothing without a partition', () => {
    const { container } = render(<OverviewTab partition={null} />)

    expect(container.textContent).toBe('')
  })
})
