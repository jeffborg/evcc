import { shallowMount, config } from "@vue/test-utils";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import Optimize from "./Optimize.vue";
import PlanExplanation from "../components/Optimize/PlanExplanation.vue";
import store from "@/store";

vi.mock("@/api", () => ({ default: { post: vi.fn(), get: vi.fn() } }));

config.global.mocks["$t"] = (a: string) => a;
config.global.mocks["$i18n"] = { locale: "en" };
config.global.stubs["router-link"] = true;

// minimal result: only what the view's computeds and the stubbed charts read
function evopt(hosted: boolean) {
  return {
    updated: "2026-11-12T00:00:00Z",
    hosted,
    req: { eta_c: 0.9, eta_d: 0.9, time_series: { dt: [900, 900] } },
    res: { status: "optimal", objective_value: -1.5 },
    details: {
      timestamp: ["2026-11-12T00:00:00Z", "2026-11-12T00:15:00Z"],
      batteryDetails: [],
      demandDetails: [],
    },
  };
}

// store.reset() only clears the keys of the initial state, not the site settings
function clearState() {
  store.update({ optimizer: undefined, evopt: undefined });
}

afterEach(clearState);

describe("optimize view", () => {
  // regression: upstream gates the explain card on `evopt.hosted`, which is false
  // for a custom OPTIMIZER_URI. This fork's self-hosted optimizer implements
  // /optimize/explain, so the card must render regardless - an upstream sync that
  // restores the gate silently removes the feature again.
  test("shows the plan explanation for a self-hosted optimizer", () => {
    store.update({ optimizer: true, evopt: evopt(false) } as any);

    const card = shallowMount(Optimize).findComponent(PlanExplanation);
    expect(card.exists()).toBe(true);
  });

  test("shows the plan explanation for the hosted optimizer", () => {
    store.update({ optimizer: true, evopt: evopt(true) } as any);

    const card = shallowMount(Optimize).findComponent(PlanExplanation);
    expect(card.exists()).toBe(true);
  });
});
