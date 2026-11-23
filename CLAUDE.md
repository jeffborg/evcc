# evcc — jeffborg fork

This is a customized fork of [evcc-io/evcc](https://github.com/evcc-io/evcc). It tracks
upstream closely and adds a set of optimizer + UI customizations for a home battery /
dynamic-tariff (Amber) setup. This file documents **what the fork changes vs upstream and
how it's maintained** — read it before touching the optimizer or resolving an upstream sync.

> For general build commands, architecture and subsystem docs, see **`AGENTS.md`** and
> **`docs/agents/`** — those are upstream's and are kept in sync, so **do not add fork notes
> there** (it would conflict on every upstream merge). This `CLAUDE.md` is fork-only and never
> conflicts; keep fork-specific guidance here.

## Fork customizations vs upstream

### Optimizer (`core/site_optimizer.go`, `core/site.go`) — the core divergence
Upstream now ships its own optimizer; the fork layers these on top of it:

- **Planner tariff drives the import price.** `optimizerRequest` reads `TariffUsagePlanner`
  (not `TariffUsageGrid`) for the optimizer's import price — production planner is
  "Amber minus the demand-window component". `fillMissingRateSlots`/`rateHorizonSlots` pad
  gaps in the planner forecast with a deliberately high fallback (`plannerRateFallback`) so
  the optimizer treats unknown slots as expensive; substituted slots are flagged in
  `requestDetails.GridForecastMissing` for the UI.
- **`PMaxImp` hardcoded to `15000`** (physical grid-connection ceiling). **Intentionally NOT**
  derived from `site.circuit.GetMaxPower()` — that returns the Amber *max-demand* value, not
  the connection limit.
- **Manual PA override** — `optimizerManualPA` field + `optimizerPA()`; a manual value
  (currency/kWh) wins over the derived `min(pN)·η·0.99`.
- **Recurring per-weekday battery SoC reserve goals** — `batteryOptimizerSocGoals`
  (`[]api.RepeatingPlan`), applied by `applyBatterySocGoals`/`batterySocGoalSlots` into
  `bat.SGoal`, weekday- and timezone-filtered (goal time is always interpreted in the goal's
  own tz, never server-local).
- **Tariff-change trigger** — the site update loop re-runs the optimizer when the
  **planner or feedin** rates change (fnv fingerprint in `optimizerTariffsChanged`), not just
  once per 15-min slot. A **one-cycle settle** (`optimizerTariffDirty`) coalesces the two
  separate MQTT topics into one consistent run; the pending-run check runs *before* re-arming
  so a steady price stream can't starve it. Runs are single-flight (`optimizerMu` +
  `optimizerPending` + `rerunIfPending`) — a trigger during a run queues exactly one re-run.
- **`batteryPower` default `10000`** W (upstream 6000).
- Runs against a self-hosted optimizer (`OPTIMIZER_URI`).

### Planner (`core/loadpoint_plan.go`, `core/loadpoint_plan_softstop.go`)
Amber bills in 5-min intervals; the planner tariff's current 15-min slot is updated each
interval (~20s in). Opt-in per loadpoint in `evcc.yaml` with **`planSoftStopBand`** (e.g. `0.2`);
**unset/0 keeps upstream's planner behaviour** (slot deferral/continuation rules, hard stop).
With a band > 0 the (non-optimizer) planner follows its slots strictly and softens marginal stops:

- **Strict slots** — `plannerActive` is only true inside a planned slot. Upstream's short-slot
  deferral, stay-until-slot-end, short-remainder and avoid-restart continuations are skipped.
  "Continue after target time" and `continuous` strategy are kept.
- **Demand-window abort** (always, independent of the band) — demand windows are left out of the planner tariff; an overrun past
  the target time into such a gap (`plannerRateGap`) finishes the plan instead of charging on.
- **Soft stop** — while active the plan's highest slot price is kept as `planRefPrice`. When
  planner charging ends in smart mode with the current planner price within
  `planSoftStopBand` (relative to |ref|) and the plan still pending, the pv timer is *reset*
  instead of elapsed (`elapsePVTimer` in Update's planner branch), so pv mode holds min
  current and runs `Disable.Delay`. A re-activated plan returns to full power; a price above
  the band, an undefined rate (demand window) or a reached goal escalate to a hard stop.
  Logic lives in the fork-only `loadpoint_plan_softstop.go`; Update has two hook lines.
- **Circuit limit ignored** for plan power (`EffectiveMaxPower`, `circuitMaxPower` commented out).
- `planner.WithClock` (fork-only `core/planner/options.go`) makes the planner testable on a
  mock clock.

### Converged with / adopted from upstream (no longer fork-unique)
- **Discharge-to-grid** (`batteryGridDischarge` → `bat.DischargeToGrid`) — the fork had this
  early; **upstream has since added an equivalent** (post-0.314.1), so it converges on the
  next sync. Don't treat it as a fork feature to defend — reconcile toward upstream's version.
- **Grid export limit** (`PMaxExp` via `GetGridExportLimit`) — upstream feature, retained.
- **echarts Optimize view** — upstream migrated the `/optimize` charts off chart.js; the fork
  adopted that (dropped its old chart.js `Optimize/PriceChart.vue` + `chartSync`).
- Upstream's key-based optimizer suggestions.

### UI
- Battery **SoC reserve goals** editor (repeating-plan style) + **manual PA** setting in the
  battery config card.
- **`robustPriceMax`** price-spike clipping in the forecast price chart
  (`assets/js/utils/robustPriceMax.ts`, used by `Forecast/PriceChart.vue`).
- **Plan explanation shown for the self-hosted optimizer.** Upstream hides the
  `Optimize/PlanExplanation.vue` card behind `v-if="evopt.hosted"` (added upstream
  2026-10-24, "hide explain for self-hosted optimizer"), where `hosted` is
  `optimizerURI() == OPTIMIZER_URI` — so it never shows with a custom `OPTIMIZER_URI`. This
  fork's optimizer implements `/optimize/explain` itself, so the gate is **dropped** in
  `assets/js/views/Optimize.vue` and guarded by `assets/js/views/Optimize.test.ts`. Only the
  UI gate is removed: `Hosted` in the published payload keeps its real value, so the
  self-hosted horizon/coarsening behaviour (no 48h clamp, `OPTIMIZER_COARSEN_AFTER`) is
  untouched. **On an upstream sync, re-check that the `v-if` has not come back.**

## Staying in sync with upstream

- **Nightly** (`.github/workflows/nightly.yml`): when fork `master` no longer merges cleanly
  with upstream, it auto-opens a `resolve/upstream-<date>` PR and pauses nightly Docker builds
  until it's merged.
- **Reconciliation strategy:** when upstream changes the optimizer (it evolves its own),
  **take upstream's version as the base and fold the fork features back in** — don't try to
  keep the fork's old optimizer and patch upstream around it. The fork's features are the
  discrete folds listed above.
- The fork **deletes** upstream's `claude-*`/`docs-*`/`documentation` workflows and keeps its
  own `nightly.yml`/`release.yml`/`default.yml`.

## Release flow
Point releases: open a PR with **base `tag-0.x.y`**; `sync-tags-pr.yml` auto-tags `0.x.y.N`,
builds the Docker image and creates the GitHub release. `tag-0.x.y` branches start from the
matching upstream tag with `master` merged in.

## Build / run notes
- UI toolchain is voidzero **`vp`** (not `npx`); **Node 26** (`engines.node >=26`). Lint:
  `vp run lint` (vue-tsc + i18n); tests `vp run test`; `vp run openapi` regenerates
  `server/openapi.state.yaml` + `server/mcp/openapi.json` from the UI State type and must be
  committed (UI job ends in a porcelain check).
- The optimizer needs sponsor authorization and a reachable `OPTIMIZER_URI`.
