import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import MonitoringDashboard from './MonitoringDashboard.jsx'

const mockMonitoringStatus = vi.fn()

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({ monitoringStatus: mockMonitoringStatus }),
  getBaseURLForPort: (port) => `http://localhost:${port}`,
}))

vi.mock('@/components/monitoring/MonitoringCharts.jsx', () => ({
  default: () => <div data-testid="uplot-charts" />,
}))

vi.mock('react-router-dom', () => ({
  useNavigate: () => vi.fn(),
}))

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

    const { container } = render(<MonitoringDashboard />)

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

    const { getByTestId } = render(<MonitoringDashboard />)
    await waitFor(() => {
      expect(getByTestId('uplot-charts')).toBeTruthy()
    })
  })
})
