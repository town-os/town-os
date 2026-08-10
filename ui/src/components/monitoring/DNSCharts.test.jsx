import { describe, it, expect, vi } from 'vitest'
import { render } from '@testing-library/react'
import DNSCharts from './DNSCharts.jsx'
import {
  RATE_INTERVAL,
  ROLODEX_SELECTOR,
  DNS_QUERY_RCODE_QUERIES,
  DNS_LATENCY_QUERIES,
  DNS_CACHE_RATIO_QUERIES,
  DNS_CACHE_ENTRY_QUERIES,
  DNS_BLOCKLIST_QUERIES,
  DNS_UPSTREAM_TIER_QUERIES,
  DNS_TRAFFIC_QUERIES,
  DNS_ANSWER_SOURCE_QUERIES,
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
  DNS_QUERY_RCODE_QUERIES,
  DNS_LATENCY_QUERIES,
  DNS_ANSWER_SOURCE_QUERIES,
  DNS_CACHE_RATIO_QUERIES,
  DNS_CACHE_ENTRY_QUERIES,
  DNS_BLOCKLIST_QUERIES,
  DNS_UPSTREAM_TIER_QUERIES,
  DNS_TRAFFIC_QUERIES,
]

describe('rolodex queries', () => {
  // The job selector is what keeps these panels pointed at the box's own
  // resolver; without it a second DNS exporter in the same Prometheus would
  // be summed into every series.
  it('selects the rolodex scrape job in every query', () => {
    for (const set of ALL_QUERY_SETS) {
      for (const q of set) {
        expect(q.expr).toContain(ROLODEX_SELECTOR)
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

  // Quantiling the raw buckets would draw one line per transport rather
  // than the box-wide latency the panel is titled for, because the bucket
  // series carry a proto label.
  it('aggregates latency buckets by le before quantiling', () => {
    expect(DNS_LATENCY_QUERIES).toHaveLength(3)
    for (const q of DNS_LATENCY_QUERIES) {
      expect(q.expr).toContain('sum by (le) (rate(rolodex_dns_query_duration_seconds_bucket')
      expect(q.expr).toMatch(/^histogram_quantile\(0\.(5|95|99), /)
    }
  })

  // Counting a cached NXDOMAIN as a hit is the point: it saved an upstream
  // round trip exactly as a positive hit did.
  it('counts negative cache hits as hits in the ratio', () => {
    const [ratio] = DNS_CACHE_RATIO_QUERIES
    expect(ratio.expr).toContain('rolodex_dns_cache_hits_total')
    expect(ratio.expr).toContain('rolodex_dns_cache_negative_hits_total')
    expect(ratio.expr).toContain('rolodex_dns_cache_misses_total')
    expect(ratio.expr.startsWith('100 * ')).toBe(true)
    // Clamping the denominator would draw a confident 0% for a cache
    // nothing has asked anything; the gap is the honest rendering.
    expect(ratio.expr).not.toContain('clamp_min')
  })
})

describe('DNSCharts', () => {
  it('renders one panel per DNS metric group', () => {
    const { getAllByTestId } = render(<DNSCharts />)
    const charts = getAllByTestId('uplot')
    expect(charts).toHaveLength(ALL_QUERY_SETS.length)

    const titles = charts.map((c) => c.getAttribute('data-title'))
    expect(titles).toEqual([
      'DNS Queries by Response Code',
      'Query Latency',
      'Answers by Source',
      'Cache Hit Ratio',
      'Cache Entries',
      'Blocklist Activity',
      'Upstream Tier Outcomes',
      'DNS Traffic',
    ])
  })

  // Units are the Grafana unit ids: a panel whose formatter is missing
  // falls through to a bare float, which for a sub-millisecond latency
  // renders as "0.00" on every point.
  it('gives every panel a unit the chart knows how to format', () => {
    const { getAllByTestId } = render(<DNSCharts />)
    const known = new Set(['bytes', 'Bps', 'bps', 'percent', 'reqps', 's', 'short'])
    for (const c of getAllByTestId('uplot')) {
      expect(known.has(c.getAttribute('data-unit'))).toBe(true)
    }
  })

  it('pins the cache hit ratio axis to 0-100', () => {
    const { getAllByTestId } = render(<DNSCharts />)
    const ratio = getAllByTestId('uplot').find(
      (c) => c.getAttribute('data-title') === 'Cache Hit Ratio',
    )
    expect(ratio.getAttribute('data-min')).toBe('0')
    expect(ratio.getAttribute('data-max')).toBe('100')
  })

  // Response codes and answer sources partition the query stream, so
  // stacking them shows the total; latency percentiles do not, and
  // stacking them would draw p99 at p50+p95+p99.
  it('stacks only the panels whose series partition a total', () => {
    const { getAllByTestId } = render(<DNSCharts />)
    const stacked = getAllByTestId('uplot')
      .filter((c) => c.getAttribute('data-stacked') === 'true')
      .map((c) => c.getAttribute('data-title'))
    expect(stacked).toEqual(['DNS Queries by Response Code', 'Answers by Source'])
  })
})
