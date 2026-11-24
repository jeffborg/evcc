package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/core/planner"
	"github.com/evcc-io/evcc/core/settings"
	"github.com/evcc-io/evcc/messenger"
	"github.com/evcc-io/evcc/util"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

const softStopDisableDelay = 8 * time.Minute

// newSoftStopLoadpoint returns a charging 3p loadpoint (16A = 11.04kW) with an
// energy plan of 5.52kWh (30min) due in 4h, planned on the given rates
func newSoftStopLoadpoint(t *testing.T, clck *clock.Mock, rates *api.Rates) *Loadpoint {
	ctrl := gomock.NewController(t)

	tf := api.NewMockTariff(ctrl)
	tf.EXPECT().Rates().DoAndReturn(func() (api.Rates, error) { return *rates, nil }).AnyTimes()

	log := util.NewLogger("foo")

	return &Loadpoint{
		log:              log,
		clock:            clck,
		charger:          api.NewMockCharger(ctrl),
		pushChan:         make(chan messenger.Event, 10), // plan overrun notification
		planner:          planner.New(log, tf, planner.WithClock(clck)),
		status:           api.StatusC,
		enabled:          true,
		minCurrent:       minA,
		maxCurrent:       maxA,
		phases:           3,
		measuredPhases:   3,
		solarShare:       1, // pv mode requires surplus for min current
		PlanSoftStopBand: 0.2,
		planTime:         clck.Now().Add(4 * time.Hour),
		planEnergy:       5.52,
		Disable: loadpoint.ThresholdConfig{
			Delay: softStopDisableDelay,
		},
	}
}

// slotRates returns consecutive 15min slots starting at start
func slotRates(start time.Time, prices ...float64) api.Rates {
	res := make(api.Rates, 0, len(prices))
	for i, p := range prices {
		res = append(res, api.Rate{
			Start: start.Add(time.Duration(i) * 15 * time.Minute),
			End:   start.Add(time.Duration(i+1) * 15 * time.Minute),
			Value: p,
		})
	}
	return res
}

func TestPlanMaxPrice(t *testing.T) {
	now := time.Now()
	assert.Equal(t, 0.3, planMaxPrice(slotRates(now, 0.1, 0.3, 0.2)))
	assert.Equal(t, -0.05, planMaxPrice(slotRates(now, -0.1, -0.05)))
}

func TestPlanSoftStopWithin(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	rates := func(p float64) api.Rates { return slotRates(now, p) }
	at := now.Add(time.Minute)

	tc := []struct {
		name  string
		rates api.Rates
		ref   float64
		band  float64
		want  bool
	}{
		{"below ref", rates(0.10), 0.12, 0.2, true},
		{"at ref", rates(0.12), 0.12, 0.2, true},
		{"within band", rates(0.14), 0.12, 0.2, true},
		{"band edge", rates(0.15), 0.125, 0.2, true},
		{"above band", rates(0.15), 0.12, 0.2, false},
		{"negative ref within band", rates(-0.09), -0.10, 0.2, true}, // band uses |ref|: -0.10+0.02
		{"negative ref above band", rates(-0.07), -0.10, 0.2, false},
		{"zero ref above zero", rates(0.01), 0, 0.2, false},
		{"undefined window", slotRates(now.Add(time.Hour), 0.01), 0.12, 0.2, false},
		{"no rates", nil, 0.12, 0.2, false},
		{"no reference", rates(0.01), planMaxPrice(nil), 0.2, false},
		{"configurable band", rates(0.17), 0.12, 0.5, true},
		{"band disabled", rates(0.10), 0.12, 0, false},
	}

	for _, tc := range tc {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, planSoftStopWithin(tc.rates, at, tc.ref, tc.band))
		})
	}
}

// TestPlannerActiveStrictSlots verifies the planner is only active inside its
// slots: no short slot deferral and no bridging of gaps before the next slot.
func TestPlannerActiveStrictSlots(t *testing.T) {
	clck := clock.NewMock()
	start := clck.Now()

	// 30min required: cheap slots 0, 2 and 3, expensive slot 1 in between
	rates := slotRates(start, 0.10, 0.50, 0.12, 0.11, 0.50, 0.50, 0.50, 0.50,
		0.50, 0.50, 0.50, 0.50, 0.50, 0.50, 0.50, 0.50)
	lp := newSoftStopLoadpoint(t, clck, &rates)

	// short remainder of a slot with a later successor is charged, not deferred
	clck.Set(start.Add(13 * time.Minute))
	assert.True(t, lp.plannerActive(rates), "short slot remainder")
	assert.Equal(t, 0.12, lp.planRefPrice, "reference is the plan's max slot price")

	// expensive slot between planned slots stops the plan
	clck.Set(start.Add(15*time.Minute + 10*time.Second))
	assert.False(t, lp.plannerActive(rates), "gap between slots")

	// no re-start guard even right before the next slot
	lp.setPlanActive(true)
	clck.Set(start.Add(29*time.Minute + 45*time.Second))
	assert.False(t, lp.plannerActive(rates), "just before next slot")
	assert.Equal(t, 0.12, lp.planRefPrice, "reference kept while inactive")

	// next slot
	clck.Set(start.Add(30*time.Minute + 10*time.Second))
	assert.True(t, lp.plannerActive(rates), "next slot")
}

// TestPlannerActiveUpstream verifies that without a soft stop band the planner
// keeps upstream's short slot deferral and re-start avoidance.
func TestPlannerActiveUpstream(t *testing.T) {
	clck := clock.NewMock()
	start := clck.Now()

	rates := slotRates(start, 0.10, 0.50, 0.12, 0.11, 0.50, 0.50, 0.50, 0.50,
		0.50, 0.50, 0.50, 0.50, 0.50, 0.50, 0.50, 0.50)
	lp := newSoftStopLoadpoint(t, clck, &rates)
	lp.PlanSoftStopBand = 0

	// short remainder of a slot with a later successor is deferred
	clck.Set(start.Add(13 * time.Minute))
	assert.False(t, lp.plannerActive(rates), "short slot remainder")

	// running plan avoids the re-start right before the next slot
	lp.setPlanActive(true)
	clck.Set(start.Add(29*time.Minute + 45*time.Second))
	assert.True(t, lp.plannerActive(rates), "just before next slot")

	// soft stop is never armed
	lp.planRefPrice = 0.12
	lp.elapsePVTimer()
	lp.planSoftStopCharging(true)
	lp.planSoftStopUpdate(api.ModeSmart, slotRates(start.Add(29*time.Minute), 0.10))
	assert.False(t, lp.planSoftStop)
	assert.Equal(t, elapsed, lp.pvTimer)
}

func TestPlanSoftStopUpdate(t *testing.T) {
	clck := clock.NewMock()
	start := clck.Now()

	tc := []struct {
		name    string
		price   *float64 // nil: undefined window
		mode    api.ChargeMode
		enabled bool
		done    bool // plan goal reached
		soft    bool
	}{
		{"within band", new(0.14), api.ModeSmart, true, false, true},
		{"below reference", new(0.05), api.ModeSmart, true, false, true},
		{"above band", new(0.15), api.ModeSmart, true, false, false},
		{"undefined window", nil, api.ModeSmart, true, false, false},
		{"not smart mode", new(0.14), api.ModeNow, true, false, false},
		{"charger disabled", new(0.14), api.ModeSmart, false, false, false},
		{"plan finished", new(0.14), api.ModeSmart, true, true, false},
	}

	for _, tc := range tc {
		t.Run(tc.name, func(t *testing.T) {
			var rates api.Rates
			if tc.price != nil {
				rates = slotRates(start, *tc.price)
			}

			lp := newSoftStopLoadpoint(t, clck, &rates)
			lp.enabled = tc.enabled
			if tc.done {
				lp.planEnergy = 0
				lp.planTime = time.Time{}
			}

			// planner charged the last cycle
			lp.planRefPrice = 0.12
			lp.elapsePVTimer()
			lp.planSoftStopCharging(true)

			lp.planSoftStopUpdate(tc.mode, rates)

			assert.Equal(t, tc.soft, lp.planSoftStop, "soft stop")
			assert.False(t, lp.planCharging, "planner charging consumed")
			if tc.soft {
				assert.True(t, lp.pvTimer.IsZero(), "pv timer reset for disable countdown")
			} else {
				assert.Equal(t, elapsed, lp.pvTimer, "pv timer elapsed for hard stop")
			}
		})
	}
}

func TestPlanSoftStopNotArmedWithoutPlanner(t *testing.T) {
	clck := clock.NewMock()
	rates := slotRates(clck.Now(), 0.10)
	lp := newSoftStopLoadpoint(t, clck, &rates)
	lp.planRefPrice = 0.12

	// min soc charging (no planner) ends hard
	lp.elapsePVTimer()
	lp.planSoftStopCharging(false)
	lp.planSoftStopUpdate(api.ModeSmart, rates)

	assert.False(t, lp.planSoftStop)
	assert.Equal(t, elapsed, lp.pvTimer)
}

func TestPlanSoftStopEscalation(t *testing.T) {
	clck := clock.NewMock()
	start := clck.Now()

	soft := func(t *testing.T, rates *api.Rates) *Loadpoint {
		lp := newSoftStopLoadpoint(t, clck, rates)
		lp.planRefPrice = 0.12
		lp.elapsePVTimer()
		lp.planSoftStopCharging(true)
		lp.planSoftStopUpdate(api.ModeSmart, *rates)
		assert.True(t, lp.planSoftStop)

		// countdown running in pv mode
		lp.pvTimer = clck.Now()
		return lp
	}

	t.Run("stays soft while within band", func(t *testing.T) {
		rates := slotRates(start, 0.14)
		lp := soft(t, &rates)
		lp.planSoftStopUpdate(api.ModeSmart, rates)
		assert.True(t, lp.planSoftStop)
		assert.Equal(t, clck.Now(), lp.pvTimer, "countdown untouched")
	})

	t.Run("price rises above band", func(t *testing.T) {
		rates := slotRates(start, 0.14)
		lp := soft(t, &rates)
		rates = slotRates(start, 0.30)
		lp.planSoftStopUpdate(api.ModeSmart, rates)
		assert.False(t, lp.planSoftStop)
		assert.Equal(t, elapsed, lp.pvTimer)
	})

	t.Run("demand window starts", func(t *testing.T) {
		rates := slotRates(start, 0.14)
		lp := soft(t, &rates)
		rates = slotRates(start.Add(time.Hour), 0.14)
		lp.planSoftStopUpdate(api.ModeSmart, rates)
		assert.False(t, lp.planSoftStop)
		assert.Equal(t, elapsed, lp.pvTimer)
	})

	t.Run("plan goal reached", func(t *testing.T) {
		rates := slotRates(start, 0.14)
		lp := soft(t, &rates)
		lp.planEnergy = 0
		lp.planTime = time.Time{}
		lp.planSoftStopUpdate(api.ModeSmart, rates)
		assert.False(t, lp.planSoftStop)
		assert.Equal(t, elapsed, lp.pvTimer)
	})

	t.Run("countdown finished", func(t *testing.T) {
		rates := slotRates(start, 0.14)
		lp := soft(t, &rates)
		lp.enabled = false
		lp.pvTimer = time.Time{}
		lp.planSoftStopUpdate(api.ModeSmart, rates)
		assert.False(t, lp.planSoftStop)
		assert.True(t, lp.pvTimer.IsZero(), "timer not touched")
	})

	t.Run("planner resumes", func(t *testing.T) {
		rates := slotRates(start, 0.14)
		lp := soft(t, &rates)
		lp.elapsePVTimer()
		lp.planSoftStopCharging(true)
		assert.False(t, lp.planSoftStop)
		assert.True(t, lp.planCharging)
	})
}

// TestPlanSoftStopPVCountdown verifies the hand-over to pv mode: a soft stop
// holds min current until the disable delay, a hard stop disables at once.
func TestPlanSoftStopPVCountdown(t *testing.T) {
	const importPower = 2000 // no surplus

	// a realistic time- the elapsed pv timer is just after the mock clock's epoch
	clck := clock.NewMock()
	clck.Set(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC))
	start := clck.Now()

	stop := func(t *testing.T, price float64) *Loadpoint {
		rates := slotRates(start, price)
		lp := newSoftStopLoadpoint(t, clck, &rates)
		lp.planRefPrice = 0.12
		lp.elapsePVTimer()
		lp.planSoftStopCharging(true)
		lp.planSoftStopUpdate(api.ModeSmart, rates)
		return lp
	}

	t.Run("soft stop", func(t *testing.T) {
		lp := stop(t, 0.14)
		defer clck.Set(start)

		assert.Equal(t, minA, lp.pvMaxCurrent(importPower, 0, false, false), "min current")
		clck.Add(softStopDisableDelay - time.Second)
		assert.Equal(t, minA, lp.pvMaxCurrent(importPower, 0, false, false), "countdown running")
		clck.Add(time.Second)
		assert.Equal(t, 0.0, lp.pvMaxCurrent(importPower, 0, false, false), "countdown elapsed")
	})

	t.Run("hard stop", func(t *testing.T) {
		lp := stop(t, 0.30)
		assert.Equal(t, 0.0, lp.pvMaxCurrent(importPower, 0, false, false))
	})
}

func TestPlanSoftStopBandConfig(t *testing.T) {
	// loadpoint yaml is decoded like NewLoadpointFromConfig does
	lp := NewLoadpoint(util.NewLogger("foo"), nil)
	assert.NoError(t, util.DecodeOther(map[string]any{"planSoftStopBand": 0.3}, lp))
	assert.Equal(t, 0.3, lp.PlanSoftStopBand)
	assert.True(t, lp.planSoftStopEnabled())

	lp = NewLoadpoint(util.NewLogger("foo"), nil)
	assert.NoError(t, util.DecodeOther(map[string]any{}, lp))
	assert.False(t, lp.planSoftStopEnabled(), "default is upstream behaviour")
}

// TestPlannerActiveOverrunDemandWindow verifies that an overrunning plan, which is
// re-planned continuously from now with missing rates padded, stops when it runs
// into a window without planner rate (demand window) instead of charging through it.
func TestPlannerActiveOverrunDemandWindow(t *testing.T) {
	for _, band := range []float64{0, 0.2} {
		t.Run(fmt.Sprintf("band %.1f", band), func(t *testing.T) {
			clck := clock.NewMock()
			clck.Set(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC))
			start := clck.Now()

			// rates for 1h, demand window 1h-2h left undefined, rates again from 2h
			rates := append(slotRates(start, 0.10, 0.10, 0.10, 0.10), slotRates(start.Add(2*time.Hour), 0.10, 0.10)...)
			lp := newSoftStopLoadpoint(t, clck, &rates)
			lp.settings = settings.NewDatabaseSettingsAdapter("foo")
			lp.PlanSoftStopBand = band

			// 20kWh until 30min cannot be met- the plan overruns
			lp.planTime = start.Add(30 * time.Minute)
			lp.planEnergy = 20

			for _, m := range []int{0, 29, 31, 45, 59} {
				clck.Set(start.Add(time.Duration(m) * time.Minute))
				assert.True(t, lp.plannerActive(rates), "t+%dm", m)
			}

			// overrun into the demand window aborts the plan
			clck.Set(start.Add(61 * time.Minute))
			assert.False(t, lp.plannerActive(rates), "demand window")
			assert.Zero(t, lp.planEnergy, "plan finished")

			// and stays stopped after the window
			clck.Set(start.Add(121 * time.Minute))
			assert.False(t, lp.plannerActive(rates), "after demand window")
		})
	}
}

// TestPlannerActiveOverrunCovered verifies that an overrunning plan keeps charging
// while planner rates are defined, and past the published horizon.
func TestPlannerActiveOverrunCovered(t *testing.T) {
	clck := clock.NewMock()
	clck.Set(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC))
	start := clck.Now()

	rates := slotRates(start, 0.10, 0.10, 0.10, 0.10)
	lp := newSoftStopLoadpoint(t, clck, &rates)
	lp.settings = settings.NewDatabaseSettingsAdapter("foo")
	lp.planTime = start.Add(30 * time.Minute)
	lp.planEnergy = 20

	for _, m := range []int{0, 31, 59, 61, 90} {
		clck.Set(start.Add(time.Duration(m) * time.Minute))
		assert.True(t, lp.plannerActive(rates), "t+%dm", m)
	}
}
