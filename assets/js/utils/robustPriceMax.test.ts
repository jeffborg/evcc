import { describe, expect, it } from "vitest";
import { robustPriceMax } from "./robustPriceMax";

describe("robustPriceMax", () => {
  // 48h of 30-min slots: mostly 5-50 c/kWh, with a 1h ($20 = 2000c) spike
  const spiky = [
    ...Array.from({ length: 94 }, (_, i) => 5 + (i % 45)), // ~5..49
    2000,
    2000, // 1h spike (2 slots)
  ];

  it("caps the axis well below the spike (P95)", () => {
    const max = robustPriceMax(spiky, { percentile: 95 });
    expect(max).toBeLessThan(100); // not 2000
    expect(max).toBeGreaterThan(40); // still above the normal band
  });

  it("does not clip a calm day (peak within the everyday range)", () => {
    const calm = Array.from({ length: 96 }, (_, i) => 5 + (i % 40)); // 5..44, no spike
    expect(robustPriceMax(calm, { percentile: 95 })).toBe(Math.max(...calm));
  });

  it("returns true max when the peak only modestly exceeds the percentile", () => {
    const gentle = [10, 12, 15, 18, 20, 22, 25, 30]; // 30 < 1.5 * P95
    expect(robustPriceMax(gentle)).toBe(30);
  });

  it("ignores non-finite values and handles empty input", () => {
    expect(robustPriceMax([])).toBe(0);
    expect(robustPriceMax([NaN, Infinity, 5, 10])).toBe(10);
  });

  it("percentile knob is honoured (P98 caps higher than P95)", () => {
    expect(robustPriceMax(spiky, { percentile: 98 })).toBeGreaterThanOrEqual(
      robustPriceMax(spiky, { percentile: 95 })
    );
  });
});
