package core

import (
	"math"
	"time"

	"github.com/evcc-io/evcc/api"
)

// fork: planner soft stop
//
// The planner starts and stops strictly on its slots. When planner charging ends
// while the current planner price is still within planSoftStopBand of the most
// expensive slot of the plan, charging is not stopped hard. Instead the pv timer
// is reset so smart mode drops to min current and runs its disable countdown
// (Disable.Delay). A re-activated plan returns to full power, a price above the
// band or an undefined tariff window (demand window) stops immediately.

// planSoftStopBand is the relative band above the plan's reference price
// within which a plan stop is handed over to the pv disable countdown
const planSoftStopBand = 0.2

// planMaxPrice returns the highest slot price of the plan
func planMaxPrice(plan api.Rates) float64 {
	res := math.Inf(-1)
	for _, slot := range plan {
		res = max(res, slot.Value)
	}
	return res
}

// planSoftStopWithin reports whether the rate at t is within the soft stop band of ref.
// Missing rates (no tariff or an undefined demand window) are never within the band.
func planSoftStopWithin(rates api.Rates, t time.Time, ref float64) bool {
	if math.IsInf(ref, 0) || math.IsNaN(ref) {
		return false
	}

	rate, err := rates.At(t)
	if err != nil {
		return false
	}

	return rate.Value <= ref+planSoftStopBand*math.Abs(ref)
}

// planPending reports whether the charging plan still requires charging
func (lp *Loadpoint) planPending() bool {
	if lp.EffectivePlanTime().IsZero() {
		return false
	}

	goal, _ := lp.GetPlanGoal()
	return lp.GetPlanRequiredDuration(goal, lp.EffectiveMaxPower()) > 0
}

// planSoftStopCharging records whether this cycle was charged by the planner
func (lp *Loadpoint) planSoftStopCharging(planner bool) {
	lp.planCharging = planner
	lp.planSoftStop = false
}

// planSoftStopUpdate runs on cycles without planner or min soc charging. When
// planner charging has just ended it decides between soft stop (pv disable
// countdown at min current) and hard stop (elapsed pv timer), and escalates a
// running soft stop to a hard stop once it is no longer justified.
func (lp *Loadpoint) planSoftStopUpdate(mode api.ChargeMode, rates api.Rates) {
	charging := lp.planCharging
	lp.planCharging = false

	within := func() bool {
		return planSoftStopWithin(rates, lp.clock.Now(), lp.planRefPrice)
	}

	switch {
	case charging:
		if mode == api.ModeSmart && lp.enabled && within() && lp.planPending() {
			lp.log.DEBUG.Printf("plan: soft stop- price within %.0f%% of %.3f, continuing at min current until pv disable delay", planSoftStopBand*100, lp.planRefPrice)
			lp.planSoftStop = true
			lp.resetPVTimer()
			return
		}

		lp.log.DEBUG.Println("plan: hard stop")

	case lp.planSoftStop:
		switch {
		case mode != api.ModeSmart || !lp.enabled:
			// mode changed or pv disable countdown finished
			lp.planSoftStop = false

		case !within() || !lp.planPending():
			lp.log.DEBUG.Println("plan: soft stop no longer justified- stopping")
			lp.planSoftStop = false
			lp.elapsePVTimer()
		}
	}
}
