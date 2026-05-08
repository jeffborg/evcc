import { describe, expect, it } from "vitest";
import PriceChart from "./PriceChart.vue";

const baseVm = {
  evopt: {
    req: {
      time_series: {
        dt: [3600, 3600],
        p_N: [0.1, 0.2],
        p_E: [0.05, 0.15],
      },
    },
  },
  timestamp: "2026-01-01T00:00:00Z",
  currency: "EUR",
  pricePerKWhUnit: () => "€/kWh",
  formatTimeRange: () => "",
  formatPrice: () => "",
  emitHoverIndex: () => undefined,
};

describe("Optimize PriceChart", () => {
  it("uses logarithmic scale only when all prices are positive", () => {
    const computed = (PriceChart as any).computed;

    expect(computed.isLogarithmicPriceScale.call(baseVm)).toBe(true);
    expect(
      computed.isLogarithmicPriceScale.call({
        ...baseVm,
        evopt: {
          req: {
            time_series: {
              ...baseVm.evopt.req.time_series,
              p_E: [0, 0.15],
            },
          },
        },
      })
    ).toBe(false);
    expect(
      computed.isLogarithmicPriceScale.call({
        ...baseVm,
        evopt: {
          req: {
            time_series: {
              ...baseVm.evopt.req.time_series,
              p_N: [-0.1, 0.2],
            },
          },
        },
      })
    ).toBe(false);
  });

  it("applies the computed y-axis scale type", () => {
    const chartOptions = (PriceChart as any).computed.chartOptions;

    const logOptions = chartOptions.call({
      ...baseVm,
      isLogarithmicPriceScale: true,
    });
    const linearOptions = chartOptions.call({
      ...baseVm,
      isLogarithmicPriceScale: false,
    });

    expect((logOptions as any).scales.y.type).toBe("logarithmic");
    expect((linearOptions as any).scales.y.type).toBe("linear");
  });
});
