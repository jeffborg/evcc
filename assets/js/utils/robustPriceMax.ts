// Robust upper bound for price charts.
//
// Dynamic tariffs (e.g. AU AEMO/Amber) occasionally spike from a few c/kWh to
// $20/kWh. Auto-scaling the axis to that peak flattens the normal range into an
// unreadable line. Instead we cap the axis at a high percentile so the everyday
// range stays readable and the rare spikes clip at the top.
//
// The cap is only applied when the peak genuinely dwarfs the bulk of the data
// (margin guard), so calm/elevated days without a spike still scale to their
// true maximum and nothing is clipped.

export interface RobustPriceMaxOptions {
  percentile?: number; // axis cap percentile, default 95 (clips ~top 5% of slots)
  margin?: number; // only clip when trueMax > margin * percentileValue, default 1.5
}

function percentileOf(sortedAsc: number[], p: number): number {
  if (sortedAsc.length === 0) return 0;
  const idx = Math.min(sortedAsc.length - 1, Math.max(0, Math.floor((p / 100) * sortedAsc.length)));
  return sortedAsc[idx] as number;
}

/**
 * Returns the value to use as the chart's upper bound: the percentile value when
 * a dominant spike is present, otherwise the true maximum (no clipping).
 */
export function robustPriceMax(values: number[], opts: RobustPriceMaxOptions = {}): number {
  const { percentile = 95, margin = 1.5 } = opts;
  const finite = values.filter((v) => Number.isFinite(v));
  if (finite.length === 0) return 0;

  const trueMax = Math.max(...finite);
  const sorted = [...finite].sort((a, b) => a - b);
  const cap = percentileOf(sorted, percentile);

  // clip only when the peak dwarfs the everyday range
  return cap > 0 && trueMax > margin * cap ? cap : trueMax;
}
