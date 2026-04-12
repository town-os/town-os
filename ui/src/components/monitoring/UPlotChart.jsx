import { useRef, useEffect, useCallback, useState } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { getBaseURLForPort } from '@/lib/client-instance.js'

const COLORS = [
  '#3b82f6', '#ef4444', '#22c55e', '#f59e0b', '#8b5cf6',
  '#06b6d4', '#ec4899', '#14b8a6', '#f97316', '#6366f1',
]

function formatBytes(val) {
  if (val == null) return ''
  const abs = Math.abs(val)
  if (abs >= 1e12) return (val / 1e12).toFixed(1) + ' TB'
  if (abs >= 1e9) return (val / 1e9).toFixed(1) + ' GB'
  if (abs >= 1e6) return (val / 1e6).toFixed(1) + ' MB'
  if (abs >= 1e3) return (val / 1e3).toFixed(1) + ' KB'
  return val.toFixed(0) + ' B'
}

function formatBps(val) {
  if (val == null) return ''
  const abs = Math.abs(val)
  if (abs >= 1e9) return (val / 1e9).toFixed(1) + ' Gbps'
  if (abs >= 1e6) return (val / 1e6).toFixed(1) + ' Mbps'
  if (abs >= 1e3) return (val / 1e3).toFixed(1) + ' Kbps'
  return val.toFixed(0) + ' bps'
}

function formatBytesPerSec(val) {
  if (val == null) return ''
  return formatBytes(val) + '/s'
}

function formatPercent(val) {
  if (val == null) return ''
  return val.toFixed(1) + '%'
}

function getValueFormatter(unit) {
  switch (unit) {
    case 'bytes': return formatBytes
    case 'Bps': return formatBytesPerSec
    case 'bps': return formatBps
    case 'percent': return formatPercent
    default: return (v) => v == null ? '' : v.toFixed(2)
  }
}

function getMonitoringBaseURL() {
  if (import.meta.env.VITE_API_URL) {
    try {
      const u = new URL(import.meta.env.VITE_API_URL)
      return `${u.protocol}//${u.hostname}:5308`
    } catch { /* fall through */ }
  }
  return `${window.location.protocol}//${window.location.hostname}:5308`
}

/**
 * Fetches data from Prometheus query_range API via the monitoring port (5308).
 */
async function queryPrometheus(query, start, end, step) {
  const base = getMonitoringBaseURL()
  const params = new URLSearchParams({
    query,
    start: start.toFixed(0),
    end: end.toFixed(0),
    step: step + 's',
  })
  const resp = await fetch(`${base}/api/v1/query_range?${params}`)
  if (!resp.ok) throw new Error(`Prometheus query failed: ${resp.status}`)
  return resp.json()
}

/**
 * A reusable uPlot time series chart that queries Prometheus directly.
 *
 * @param {Object} props
 * @param {Array<{expr: string, legend: string}>} props.queries - PromQL queries with legend templates
 * @param {string} props.title - Chart title
 * @param {string} props.unit - Value unit: 'bytes', 'Bps', 'bps', 'percent'
 * @param {boolean} [props.stacked] - Whether to stack series
 * @param {number} [props.min] - Y-axis minimum
 * @param {number} [props.max] - Y-axis maximum
 * @param {number} [props.rangeSeconds] - Time range in seconds (default: 3600)
 * @param {number} [props.refreshMs] - Refresh interval in ms (default: 30000)
 */
export default function UPlotChart({
  queries,
  title,
  unit = 'bytes',
  stacked = false,
  min,
  max,
  rangeSeconds = 3600,
  refreshMs = 30000,
}) {
  const containerRef = useRef(null)
  const chartRef = useRef(null)
  const [error, setError] = useState(null)

  const valueFmt = getValueFormatter(unit)

  const fetchData = useCallback(async () => {
    const now = Date.now() / 1000
    const start = now - rangeSeconds
    const step = Math.max(15, Math.floor(rangeSeconds / 200))

    try {
      const results = await Promise.all(
        queries.map((q) => queryPrometheus(q.expr, start, now, step))
      )

      // Build unified timestamps array.
      let allTimestamps = new Set()
      const seriesData = []

      for (let qi = 0; qi < results.length; qi++) {
        const matrix = results[qi]?.data?.result || []
        for (const series of matrix) {
          const legendTemplate = queries[qi].legend
          let label = legendTemplate
          if (series.metric) {
            for (const [k, v] of Object.entries(series.metric)) {
              label = label.replace(`{{${k}}}`, v)
            }
          }
          // Remove any unresolved template vars.
          label = label.replace(/\{\{[^}]+\}\}/g, '')

          const vals = {}
          for (const [ts, v] of series.values) {
            allTimestamps.add(ts)
            vals[ts] = parseFloat(v)
          }
          seriesData.push({ label, vals })
        }
      }

      if (allTimestamps.size === 0) {
        setError(null)
        return
      }

      const timestamps = Array.from(allTimestamps).sort((a, b) => a - b)
      const data = [timestamps]
      for (const s of seriesData) {
        data.push(timestamps.map((ts) => s.vals[ts] ?? null))
      }

      const series = [{}] // timestamp series
      for (let i = 0; i < seriesData.length; i++) {
        series.push({
          label: seriesData[i].label,
          stroke: COLORS[i % COLORS.length],
          width: 1.5,
          fill: stacked ? COLORS[i % COLORS.length] + '33' : undefined,
          value: (_, v) => valueFmt(v),
        })
      }

      const opts = {
        width: containerRef.current?.clientWidth || 600,
        height: Math.max(150, (containerRef.current?.clientHeight || 280) - 65),
        title,
        cursor: { show: true },
        scales: {
          y: {
            auto: min == null && max == null,
            range: min != null || max != null ? [min ?? 0, max ?? 100] : undefined,
          },
        },
        axes: [
          {},
          { values: (_, ticks) => ticks.map(valueFmt) },
        ],
        series,
      }

      if (chartRef.current) {
        chartRef.current.destroy()
      }

      if (containerRef.current) {
        containerRef.current.innerHTML = ''
        chartRef.current = new uPlot(opts, data, containerRef.current)
      }

      setError(null)
    } catch (err) {
      setError(err.message)
    }
  }, [queries, title, unit, stacked, min, max, rangeSeconds, valueFmt])

  useEffect(() => {
    fetchData()
    const interval = setInterval(fetchData, refreshMs)
    return () => {
      clearInterval(interval)
      if (chartRef.current) {
        chartRef.current.destroy()
        chartRef.current = null
      }
    }
  }, [fetchData, refreshMs])

  // Resize handler.
  useEffect(() => {
    function onResize() {
      if (chartRef.current && containerRef.current) {
        chartRef.current.setSize({
          width: containerRef.current.clientWidth,
          height: Math.max(150, containerRef.current.clientHeight - 65),
        })
      }
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  if (error) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {error}
      </div>
    )
  }

  return <div ref={containerRef} className="h-full w-full" />
}
