import { describe, it, expect } from 'vitest'
import { axisScale } from './axis.js'

describe('axisScale', () => {
  it('leaves uPlot autoscaling when neither bound is given', () => {
    expect(axisScale(null, null)).toEqual({ auto: true, range: undefined })
    expect(axisScale(undefined, undefined)).toEqual({ auto: true, range: undefined })
  })

  it('pins both ends, and stops autoscaling, when both are given', () => {
    const { auto, range } = axisScale(0, 1)
    expect(auto).toBe(false)
    expect(range(null, -5, 42)).toEqual([0, 1])
  })

  // The regression this file exists for: a panel that pins its floor at zero
  // is not thereby claiming a ceiling. Defaulting the missing end to 100 --
  // which the chart used to do -- clipped Controller CPU, whose reading is per
  // core-second and passes 100 the moment the controller uses more than one
  // core, to a flat line at the top of its own panel.
  it('lets the unbounded end follow the data', () => {
    expect(axisScale(0, null).range(null, -5, 240)).toEqual([0, 240])
    expect(axisScale(null, 100).range(null, -5, 42)).toEqual([-5, 100])
  })

  // With auto off, uPlot never derives the data extents and would hand the
  // callback the scale's own (initially null) bounds instead of the numbers it
  // is there to pass through.
  it('keeps autoscaling on for a one-sided bound', () => {
    expect(axisScale(0, null).auto).toBe(true)
    expect(axisScale(null, 100).auto).toBe(true)
  })

  // uPlot passes nulls for a series with no points: every panel's first
  // render, and every panel on a box whose Prometheus has not scraped yet.
  it('survives a series with no data', () => {
    expect(axisScale(0, null).range(null, null, null)).toEqual([0, 1])
  })
})
