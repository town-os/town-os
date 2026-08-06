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

  it('renders nothing without a partition', () => {
    const { container } = render(<OverviewTab partition={null} />)

    expect(container.textContent).toBe('')
  })
})
