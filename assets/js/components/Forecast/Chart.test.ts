import { describe, expect, it } from "vitest";
import Chart from "./Chart.vue";

describe("Forecast Chart", () => {
  it("uses logarithmic price scale only for positive grid prices", () => {
    const isLogarithmicPriceScale = (Chart as any).computed.isLogarithmicPriceScale;

    expect(
      isLogarithmicPriceScale.call({
        gridSlots: [{ value: 0.1 }, { value: 0.5 }, { value: 20 }],
      })
    ).toBe(true);
    expect(
      isLogarithmicPriceScale.call({
        gridSlots: [{ value: 0.1 }, { value: 0 }],
      })
    ).toBe(false);
    expect(
      isLogarithmicPriceScale.call({
        gridSlots: [{ value: -0.1 }, { value: 0.5 }],
      })
    ).toBe(false);
  });
});
