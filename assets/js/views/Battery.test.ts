import { shallowMount, config } from "@vue/test-utils";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import Battery from "./Battery.vue";
import BatteryConfigCard from "../components/Battery/BatteryConfigCard.vue";
import store from "@/store";
import { CURRENCY, type RepeatingPlan } from "@/types/evcc";

// the view loads battery history on mount; irrelevant here
vi.mock("@/api", () => ({ default: { get: vi.fn().mockResolvedValue({ data: [] }) } }));

config.global.mocks["$t"] = (a: string) => a;

const BATTERY = { devices: [{ controllable: true, capacity: 10, soc: 50 }] };
const GOALS: RepeatingPlan[] = [
  { weekdays: [1, 2, 3], time: "06:00", soc: 40, active: true, tz: "Australia/Sydney" },
];

// store.reset() only clears the keys of the initial state, not the site settings
function clearState() {
  store.update({
    battery: undefined,
    batteryOptimizerSocGoals: undefined,
    optimizerManualPA: undefined,
    currency: undefined,
  });
}

// shallow: only the props handed to BatteryConfigCard are of interest here
function configCard() {
  return shallowMount(Battery).findComponent(BatteryConfigCard);
}

afterEach(clearState);

describe("battery view", () => {
  // regression: the optimizer props were missing from the template, so the
  // reserve goal editor stayed empty and "add goal" appeared to do nothing
  test("passes optimizer settings to the config card", () => {
    store.update({
      battery: BATTERY,
      batteryOptimizerSocGoals: GOALS,
      optimizerManualPA: 0.05,
      currency: CURRENCY.AUD,
    });

    const card = configCard();
    expect(card.exists()).toBe(true);
    expect(card.props("batteryOptimizerSocGoals")).toEqual(GOALS);
    expect(card.props("optimizerManualPA")).toBe(0.05);
    expect(card.props("currency")).toBe(CURRENCY.AUD);
  });

  test("falls back to safe defaults when the state is empty", () => {
    store.update({ battery: BATTERY });

    const card = configCard();
    expect(card.props("batteryOptimizerSocGoals")).toEqual([]);
    expect(card.props("optimizerManualPA")).toBeNull();
    expect(card.props("currency")).toBe(CURRENCY.EUR);
  });
});
