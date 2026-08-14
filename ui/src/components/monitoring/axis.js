// Y-scale helper for the uPlot panels. It lives in its own module rather than
// inside UPlotChart.jsx so it can be tested without a canvas, and because a
// component file may only export components (react-refresh).

/**
 * Build the uPlot y-scale config for a panel's optional bounds.
 *
 * A one-sided bound must stay one-sided. A panel that pins its floor at zero —
 * CPU, where the meaningful reading is the distance above idle — is not
 * thereby claiming a ceiling, and defaulting the missing end to 100 would clip
 * a controller using more than one core to a flat line at the top of its own
 * panel, which is exactly the runaway the panel exists to show.
 *
 * `auto` stays on unless *both* ends are pinned: with it off, uPlot never
 * derives the data extents, and the range callback below would be handed the
 * scale's own (initially null) bounds instead of the numbers it is there to
 * pass through.
 *
 * @param {number} [min] - Y-axis minimum, or null/undefined to follow the data
 * @param {number} [max] - Y-axis maximum, or null/undefined to follow the data
 * @returns {{auto: boolean, range: undefined | ((u: object, dataMin: number, dataMax: number) => [number, number])}}
 */
export function axisScale(min, max) {
  if (min == null && max == null) {
    return { auto: true, range: undefined }
  }
  return {
    auto: min == null || max == null,
    // The data-derived fallbacks are themselves defaulted, because uPlot
    // passes nulls for a series with no points yet — every panel's first
    // render, and every panel on a box whose Prometheus has not scraped
    // anything.
    range: (_u, dataMin, dataMax) => [min ?? dataMin ?? 0, max ?? dataMax ?? 1],
  }
}
