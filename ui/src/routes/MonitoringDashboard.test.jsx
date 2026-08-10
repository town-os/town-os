import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import MonitoringDashboard from './MonitoringDashboard.jsx'

const mockMonitoringStatus = vi.fn()

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({ monitoringStatus: mockMonitoringStatus }),
  getBaseURLForPort: (port) => `http://localhost:${port}`,
}))

vi.mock('@/components/monitoring/MonitoringCharts.jsx', () => ({
  default: () => <div data-testid="uplot-charts" />,
}))

vi.mock('@/components/monitoring/DNSCharts.jsx', () => ({
  default: () => <div data-testid="dns-charts" />,
}))

// A real router rather than a mocked one: the tab lives in the query
// string, so a stubbed useSearchParams would test the stub.
function renderAt(path = '/dashboard/monitoring') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <MonitoringDashboard />
    </MemoryRouter>,
  )
}

describe('MonitoringDashboard', () => {
  beforeEach(() => {
    mockMonitoringStatus.mockReset()
  })

  it('embeds the Grafana iframe with refresh=30s so auto-refresh defaults on', async () => {
    mockMonitoringStatus.mockResolvedValue({
      backend: 'grafana',
      prometheus: true,
      node_exporter: true,
      grafana: true,
    })

    const { container } = renderAt()

    let iframe
    await waitFor(() => {
      iframe = container.querySelector('iframe[title="Grafana Dashboard"]')
      expect(iframe).toBeTruthy()
    })

    const src = iframe.getAttribute('src')
    expect(src).toContain('http://localhost:5308/d/town-os-overview/town-os-overview')
    expect(src).toContain('kiosk')
    expect(src).toContain('theme=light')
    expect(src).toContain('refresh=30s')
  })

  it('falls back to uPlot charts when backend is uplot', async () => {
    mockMonitoringStatus.mockResolvedValue({
      backend: 'uplot',
      prometheus: true,
      node_exporter: true,
      disk_devices: ['sda3'],
    })

    const { getByTestId } = renderAt()
    await waitFor(() => {
      expect(getByTestId('uplot-charts')).toBeTruthy()
    })
  })

  // ?tab=dns must select the DNS dashboard on load, not just when clicked:
  // the tab is in the URL so an operator can link to it and so a reload
  // during an outage comes back to the panel they were reading.
  it('opens the DNS charts directly from ?tab=dns', async () => {
    mockMonitoringStatus.mockResolvedValue({
      backend: 'uplot',
      prometheus: true,
      node_exporter: true,
      disk_devices: ['sda3'],
    })

    const { getByTestId, queryByTestId } = renderAt('/dashboard/monitoring?tab=dns')
    await waitFor(() => {
      expect(getByTestId('dns-charts')).toBeTruthy()
    })
    expect(queryByTestId('uplot-charts')).toBeNull()
  })

  it('switches the uPlot charts when the DNS tab is clicked', async () => {
    mockMonitoringStatus.mockResolvedValue({
      backend: 'uplot',
      prometheus: true,
      node_exporter: true,
      disk_devices: ['sda3'],
    })

    const { getByTestId, queryByTestId } = renderAt()
    await waitFor(() => {
      expect(getByTestId('uplot-charts')).toBeTruthy()
    })

    await userEvent.click(screen.getByRole('tab', { name: 'DNS' }))

    await waitFor(() => {
      expect(getByTestId('dns-charts')).toBeTruthy()
    })
    expect(queryByTestId('uplot-charts')).toBeNull()
  })

  // In Grafana mode the tab has to repoint the iframe at the other
  // dashboard's uid. Sharing one uid across both tabs would silently show
  // the host overview under a tab labelled DNS.
  it('points the Grafana iframe at the DNS dashboard uid on the DNS tab', async () => {
    mockMonitoringStatus.mockResolvedValue({
      backend: 'grafana',
      prometheus: true,
      node_exporter: true,
      grafana: true,
    })

    const { container } = renderAt('/dashboard/monitoring?tab=dns')

    await waitFor(() => {
      const iframe = container.querySelector('iframe[title="Grafana Dashboard"]')
      expect(iframe).toBeTruthy()
      expect(iframe.getAttribute('src')).toContain('/d/town-os-dns/town-os-dns')
    })
  })

  // An unknown tab value is a typo or a stale link, not a reason to render
  // an empty page.
  it('falls back to the system tab for an unknown ?tab= value', async () => {
    mockMonitoringStatus.mockResolvedValue({
      backend: 'uplot',
      prometheus: true,
      node_exporter: true,
      disk_devices: ['sda3'],
    })

    const { getByTestId } = renderAt('/dashboard/monitoring?tab=nonsense')
    await waitFor(() => {
      expect(getByTestId('uplot-charts')).toBeTruthy()
    })
  })
})
