import { describe, it, expect, vi } from 'vitest'
import { render } from '@testing-library/react'
import ControllerCharts from './ControllerCharts.jsx'
import {
  RATE_INTERVAL,
  CONTROLLER_SELECTOR,
  CONTROLLER_UNIT_STATE_QUERIES,
  CONTROLLER_UNIT_HEALTH_QUERIES,
  CONTROLLER_HTTP_QUERIES,
  CONTROLLER_LATENCY_QUERIES,
  CONTROLLER_AUDIT_QUERIES,
  CONTROLLER_FAILURE_QUERIES,
  CONTROLLER_DISK_QUERIES,
  CONTROLLER_DISK_FILL_QUERIES,
  CONTROLLER_CPU_QUERIES,
  CONTROLLER_MEMORY_QUERIES,
  CONTROLLER_CONCURRENCY_QUERIES,
  CONTROLLER_UPTIME_QUERIES,
  CONTROLLER_INVENTORY_QUERIES,
  CONTROLLER_ACCOUNT_QUERIES,
} from './queries.js'

vi.mock('./UPlotChart.jsx', () => ({
  default: ({ title, unit, stacked, min, max, queries }) => (
    <div
      data-testid="uplot"
      data-title={title}
      data-unit={unit}
      data-stacked={String(Boolean(stacked))}
      data-min={min == null ? '' : String(min)}
      data-max={max == null ? '' : String(max)}
      data-queries={JSON.stringify(queries)}
    />
  ),
}))

const ALL_QUERY_SETS = [
  CONTROLLER_UNIT_HEALTH_QUERIES,
  CONTROLLER_UNIT_STATE_QUERIES,
  CONTROLLER_HTTP_QUERIES,
  CONTROLLER_LATENCY_QUERIES,
  CONTROLLER_FAILURE_QUERIES,
  CONTROLLER_AUDIT_QUERIES,
  CONTROLLER_DISK_QUERIES,
  CONTROLLER_DISK_FILL_QUERIES,
  CONTROLLER_CPU_QUERIES,
  CONTROLLER_MEMORY_QUERIES,
  CONTROLLER_CONCURRENCY_QUERIES,
  CONTROLLER_UPTIME_QUERIES,
  CONTROLLER_INVENTORY_QUERIES,
  CONTROLLER_ACCOUNT_QUERIES,
]

describe('controller queries', () => {
  // The job selector is what keeps these panels pointed at this box's
  // controller rather than at anything else that lands in the same
  // Prometheus exporting the same family names.
  it('selects the controller scrape job in every query', () => {
    for (const set of ALL_QUERY_SETS) {
      for (const q of set) {
        expect(q.expr).toContain(CONTROLLER_SELECTOR)
      }
    }
  })

  // Grafana expands $__rate_interval per panel; this frontend has no macro
  // expansion, so a leaked macro is a Prometheus parse error and a blank tab.
  it('pins a literal rate window rather than the Grafana macro', () => {
    for (const set of ALL_QUERY_SETS) {
      for (const q of set) {
        expect(q.expr).not.toContain('$__rate_interval')
        if (q.expr.includes('rate(')) {
          expect(q.expr).toContain(`[${RATE_INTERVAL}]`)
        }
      }
    }
  })

  it('labels every series', () => {
    for (const set of ALL_QUERY_SETS) {
      for (const q of set) {
        expect(q.legend).toBeTruthy()
      }
    }
  })

  // townos_http_requests_total carries method as well as status. A status
  // panel that did not sum method away would draw a line per pair, which on
  // a control plane answering half a dozen methods is a legend nobody reads.
  it('sums away the labels the panel does not legend on', () => {
    const [byStatus] = CONTROLLER_HTTP_QUERIES
    expect(byStatus.expr).toContain('sum by (status) (')
    expect(byStatus.expr).not.toContain('method')

    const [byResult] = CONTROLLER_AUDIT_QUERIES
    expect(byResult.expr).toContain('sum by (result) (')
  })

  // Uptime is computed against the scraper's clock rather than exported as
  // its own counter, so a restart shows as the drop to zero.
  it('derives uptime from the start-time gauge', () => {
    const [uptime] = CONTROLLER_UPTIME_QUERIES
    expect(uptime.expr).toBe(`time() - townos_start_time_seconds${CONTROLLER_SELECTOR}`)
  })

  // Used and available stack to the filesystem size. Adding the total as a
  // third series would draw that size twice on a stacked panel.
  it('graphs disk as used and available, not used and total', () => {
    const exprs = CONTROLLER_DISK_QUERIES.map((q) => q.expr).join(' ')
    expect(exprs).toContain('townos_disk_used_bytes')
    expect(exprs).toContain('townos_disk_available_bytes')
    expect(exprs).not.toContain('townos_disk_total_bytes')
  })

  // The fill panel is the one place the total is the right denominator: it is
  // what "how full" is a fraction of.
  it('measures disk fill against the filesystem size', () => {
    const [fill] = CONTROLLER_DISK_FILL_QUERIES
    expect(fill.expr).toBe(
      `100 * townos_disk_used_bytes${CONTROLLER_SELECTOR} / townos_disk_total_bytes${CONTROLLER_SELECTOR}`,
    )
  })

  // A mean out of two counters. Inverting the ratio, or dividing by a
  // differently-aggregated denominator, still returns a number — just the
  // wrong one, on a panel whose unit makes it look plausible.
  it('averages latency as seconds over requests, aggregated the same way', () => {
    const [latency] = CONTROLLER_LATENCY_QUERIES
    const [numerator, denominator] = latency.expr.split(' / ')
    expect(numerator).toContain('townos_http_request_seconds_total')
    expect(denominator).toContain('townos_http_requests_total')
    expect(numerator).toContain('sum by (method)')
    expect(denominator).toContain('sum by (method)')
  })
})

describe('ControllerCharts', () => {
  it('renders one panel per controller metric group', () => {
    const { getAllByTestId } = render(<ControllerCharts />)
    const charts = getAllByTestId('uplot')
    expect(charts).toHaveLength(ALL_QUERY_SETS.length)

    // The order is the Grafana grid order, which is the order an operator
    // reads them in when something is wrong: what is down, what the API is
    // doing, what is failing, the disk, then the controller process itself.
    // The inventory counts sit last because they do not move on a working box.
    const titles = charts.map((c) => c.getAttribute('data-title'))
    expect(titles).toEqual([
      'Service Health',
      'Service Units by State',
      'API Requests by Status',
      'API Latency',
      'Recent Failures',
      'Audit Events',
      'Town OS Disk Usage',
      'Town OS Disk Fill',
      'Controller CPU',
      'Controller Memory',
      'Controller Concurrency',
      'Controller Uptime',
      'Inventory',
      'Accounts',
    ])
  })

  // Units are the Grafana unit ids: a panel whose formatter is missing falls
  // through to a bare float, which for a byte count renders as a 12-digit
  // number on every point.
  it('gives every panel a unit the chart knows how to format', () => {
    const { getAllByTestId } = render(<ControllerCharts />)
    const known = new Set(['bytes', 'Bps', 'bps', 'percent', 'reqps', 's', 'short'])
    for (const c of getAllByTestId('uplot')) {
      expect(known.has(c.getAttribute('data-unit'))).toBe(true)
    }
  })

  // The health metric is a boolean per unit. Autoscaled, a wholly healthy
  // box draws as noise around 1.0 — alarming exactly when nothing is wrong.
  it('pins the service health axis to 0-1', () => {
    const { getAllByTestId } = render(<ControllerCharts />)
    const health = getAllByTestId('uplot').find(
      (c) => c.getAttribute('data-title') === 'Service Health',
    )
    expect(health.getAttribute('data-min')).toBe('0')
    expect(health.getAttribute('data-max')).toBe('1')
  })

  // Stacking is for series that partition a total: status classes, audit
  // results, and the two halves of the filesystem. Unit counts do not — system
  // and package units are separate totals, and stacking them would draw a
  // height that counts nothing. Accounts is not stacked either, despite the
  // kinds partitioning the list: the grant count riding along on that panel is
  // a subset of the user bucket, and stacked it would push the total above the
  // number of accounts that exist.
  it('stacks only the panels whose series partition a total', () => {
    const { getAllByTestId } = render(<ControllerCharts />)
    const stacked = getAllByTestId('uplot')
      .filter((c) => c.getAttribute('data-stacked') === 'true')
      .map((c) => c.getAttribute('data-title'))
    expect(stacked).toEqual([
      'API Requests by Status',
      'Audit Events',
      'Town OS Disk Usage',
    ])
  })

  // Percent panels: fill is a fraction of one filesystem and cannot exceed
  // 100, while CPU is per core-second and a controller using two cores reads
  // 200. Capping the second would clip exactly the runaway it exists to show.
  it('bounds the fill axis at 100 and leaves the CPU axis open', () => {
    const { getAllByTestId } = render(<ControllerCharts />)
    const panel = (title) =>
      getAllByTestId('uplot').find((c) => c.getAttribute('data-title') === title)

    expect(panel('Town OS Disk Fill').getAttribute('data-min')).toBe('0')
    expect(panel('Town OS Disk Fill').getAttribute('data-max')).toBe('100')
    expect(panel('Controller CPU').getAttribute('data-min')).toBe('0')
    expect(panel('Controller CPU').getAttribute('data-max')).toBe('')
  })
})
