import { describe, it, expect, vi } from 'vitest'
import { render } from '@testing-library/react'
import MonitoringCharts, { buildDiskIOQueries } from './MonitoringCharts.jsx'

vi.mock('./UPlotChart.jsx', () => ({
  default: ({ title, queries }) => (
    <div data-testid="uplot" data-title={title} data-queries={JSON.stringify(queries)} />
  ),
}))

describe('buildDiskIOQueries', () => {
  it('joins device names into a sum(rate(...)) query', () => {
    const queries = buildDiskIOQueries(['sda3', 'nvme0n1p3'])
    expect(queries).toHaveLength(2)
    expect(queries[0].legend).toBe('Read')
    expect(queries[0].expr).toBe(
      'sum(rate(node_disk_read_bytes_total{device=~"sda3|nvme0n1p3"}[5m]))',
    )
    expect(queries[1].legend).toBe('Write')
    expect(queries[1].expr).toBe(
      'sum(rate(node_disk_written_bytes_total{device=~"sda3|nvme0n1p3"}[5m]))',
    )
  })

  it('falls back to the no-devices sentinel when the list is empty', () => {
    const queries = buildDiskIOQueries([])
    expect(queries[0].expr).toContain('__no_btrfs_devices__')
    expect(queries[1].expr).toContain('__no_btrfs_devices__')
  })

  it('falls back to the sentinel when the list is undefined', () => {
    const queries = buildDiskIOQueries(undefined)
    expect(queries[0].expr).toContain('__no_btrfs_devices__')
  })
})

describe('MonitoringCharts', () => {
  it('passes the disk-device-derived queries to the Disk I/O panel', () => {
    const { getAllByTestId } = render(<MonitoringCharts diskDevices={['sda3']} />)
    const charts = getAllByTestId('uplot')
    const diskChart = charts.find(c => c.getAttribute('data-title') === 'Disk I/O (/town-os)')
    expect(diskChart).toBeTruthy()
    const queries = JSON.parse(diskChart.getAttribute('data-queries'))
    expect(queries[0].expr).toBe(
      'sum(rate(node_disk_read_bytes_total{device=~"sda3"}[5m]))',
    )
    expect(queries[1].expr).toBe(
      'sum(rate(node_disk_written_bytes_total{device=~"sda3"}[5m]))',
    )
  })
})
