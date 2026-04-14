import { describe, it, expect, vi } from 'vitest'
import { render } from '@testing-library/react'
import MonitoringCharts from './MonitoringCharts.jsx'
import { buildDiskIOQueries, NETWORK_QUERIES } from './queries.js'

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

describe('NETWORK_QUERIES', () => {
  it('only includes interfaces that are currently up', () => {
    expect(NETWORK_QUERIES).toHaveLength(2)
    for (const q of NETWORK_QUERIES) {
      expect(q.expr).toContain('and on (device) (node_network_up == 1)')
      expect(q.expr).toContain('device!~"lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*"')
      expect(q.expr).toMatch(/\) \* 8$/)
    }
    expect(NETWORK_QUERIES[0].expr).toContain('node_network_receive_bytes_total')
    expect(NETWORK_QUERIES[1].expr).toContain('node_network_transmit_bytes_total')
    expect(NETWORK_QUERIES[0].legend).toBe('{{device}} Rx')
    expect(NETWORK_QUERIES[1].legend).toBe('{{device}} Tx')
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

  it('passes the active-interface network queries to the Network panel', () => {
    const { getAllByTestId } = render(<MonitoringCharts diskDevices={['sda3']} />)
    const charts = getAllByTestId('uplot')
    const netChart = charts.find(c => c.getAttribute('data-title') === 'Network (External)')
    expect(netChart).toBeTruthy()
    const queries = JSON.parse(netChart.getAttribute('data-queries'))
    expect(queries[0].expr).toContain('and on (device) (node_network_up == 1)')
    expect(queries[1].expr).toContain('and on (device) (node_network_up == 1)')
  })
})
