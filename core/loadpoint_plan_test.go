package core

import (
	"testing"
	"time"

	evbus "github.com/asaskevich/EventBus"
	"github.com/benbjohnson/clock"
	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/planner"
	"github.com/evcc-io/evcc/tariff"
	"github.com/evcc-io/evcc/util"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// TestPlannerActiveTariffGap verifies that an overrunning plan does not keep charging
// while the planner tariff has no slot data for the current period (e.g. a demand-tariff
// peak window). Without real slot data the planner falls back to a synthetic continuous
// slot, which would only drain the home battery.
func TestPlannerActiveTariffGap(t *testing.T) {
	Voltage = 230 // V

	tc := []struct {
		name   string
		rates  api.Rates // planner tariff rates
		expect bool      // expected plannerActive result
	}{
		{
			name:   "no slot for current period during overrun- stop",
			rates:  api.Rates{{Start: hoursFromNow(5), End: hoursFromNow(6), Value: 0.1}},
			expect: false,
		},
		{
			name:   "slot covers current period- keep charging",
			rates:  api.Rates{{Start: hoursFromNow(0), End: hoursFromNow(1), Value: 0.1}},
			expect: true,
		},
	}

	for _, tc := range tc {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewMock()
			ctrl := gomock.NewController(t)

			trf := api.NewMockTariff(ctrl)
			trf.EXPECT().Type().AnyTimes().Return(api.TariffTypePriceForecast)
			trf.EXPECT().Rates().AnyTimes().Return(shift(tc.rates, clk.Now()), nil)

			uiChan, pushChan, lpChan := createChannels(t)

			lp := &Loadpoint{
				log:        util.NewLogger("foo"),
				bus:        evbus.New(),
				clock:      clk,
				minCurrent: minA,
				maxCurrent: maxA,
				phases:     1,
				status:     api.StatusC, // connected
				planner:    planner.New(util.NewLogger("foo"), trf, planner.WithClock(clk)),
				site:       &Site{tariffs: &tariff.Tariffs{Planner: trf}},
				// target in 1h but ~2.7h of charging required => overrun
				planTime:   clk.Now().Add(time.Hour),
				planEnergy: 10, // kWh
			}
			attachChannels(lp, uiChan, pushChan, lpChan)

			assert.Equal(t, tc.expect, lp.plannerActive())
		})
	}
}

// TestPlannerBridge verifies the min-current bridge: when the plan has no active slot
// right now but the next slot starts within planBridgeDuration and the charger is
// already enabled, plannerActive returns false (not a full-power slot) but flags
// planBridging so Update holds min current instead of stopping.
func TestPlannerBridge(t *testing.T) {
	Voltage = 230 // V

	min := func(m int) time.Time { return time.Time{}.Add(time.Duration(m) * time.Minute) }

	// cheap window (value 1) embedded in an otherwise expensive day (value 100)
	near := api.Rates{
		{Start: min(0), End: min(5), Value: 100},
		{Start: min(5), End: min(10), Value: 1},
		{Start: min(10), End: min(15), Value: 1},
		{Start: min(15), End: min(120), Value: 100}, // until ~2h
	}
	far := api.Rates{
		{Start: min(0), End: min(30), Value: 100},
		{Start: min(30), End: min(40), Value: 1},
		{Start: min(40), End: min(120), Value: 100},
	}

	tc := []struct {
		name    string
		rates   api.Rates
		enabled bool
		bridge  bool
	}{
		{"next slot soon while charging- bridge", near, true, true},
		{"next slot soon but not charging- no bridge", near, false, false},
		{"next slot too far- no bridge", far, true, false},
	}

	for _, tc := range tc {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewMock()
			ctrl := gomock.NewController(t)

			trf := api.NewMockTariff(ctrl)
			trf.EXPECT().Type().AnyTimes().Return(api.TariffTypePriceForecast)
			trf.EXPECT().Rates().AnyTimes().Return(shift(tc.rates, clk.Now()), nil)

			uiChan, pushChan, lpChan := createChannels(t)

			lp := &Loadpoint{
				log:        util.NewLogger("foo"),
				bus:        evbus.New(),
				clock:      clk,
				enabled:    tc.enabled,
				minCurrent: minA,
				maxCurrent: maxA,
				phases:     1,
				status:     api.StatusC, // connected
				planner:    planner.New(util.NewLogger("foo"), trf, planner.WithClock(clk)),
				site:       &Site{tariffs: &tariff.Tariffs{Planner: trf}},
				// plenty of time => no overrun, only the cheap window is selected
				planTime:   clk.Now().Add(2 * time.Hour),
				planEnergy: 0.6, // kWh => ~10min of charging at 3.68kW
			}
			attachChannels(lp, uiChan, pushChan, lpChan)

			assert.False(t, lp.plannerActive(), "no active slot expected")
			assert.Equal(t, tc.bridge, lp.planBridging)
		})
	}
}

// TestUpdatePlanBridge drives the full Update() strategy switch and verifies that a set
// planBridging flag routes to minCharging (i.e. the charger is held at min current
// instead of being stopped) - covering the loadpoint.go switch case end-to-end.
func TestUpdatePlanBridge(t *testing.T) {
	Voltage = 230 // V

	min := func(m int) time.Time { return time.Time{}.Add(time.Duration(m) * time.Minute) }

	// cheap window starting in 5min => not active now, next slot within bridge window
	rr := api.Rates{
		{Start: min(0), End: min(5), Value: 100},
		{Start: min(5), End: min(10), Value: 1},
		{Start: min(10), End: min(15), Value: 1},
		{Start: min(15), End: min(120), Value: 100},
	}

	clk := clock.NewMock()
	ctrl := gomock.NewController(t)

	charger := api.NewMockCharger(ctrl)
	charger.EXPECT().Status().AnyTimes().Return(api.StatusC, nil)
	charger.EXPECT().Enabled().AnyTimes().Return(true, nil)
	charger.EXPECT().Enable(gomock.Any()).AnyTimes().Return(nil)
	// the assertion: bridge holds min current
	charger.EXPECT().MaxCurrent(int64(minA)).MinTimes(1).Return(nil)

	trf := api.NewMockTariff(ctrl)
	trf.EXPECT().Type().AnyTimes().Return(api.TariffTypePriceForecast)
	trf.EXPECT().Rates().AnyTimes().Return(shift(rr, clk.Now()), nil)

	uiChan, pushChan, lpChan := createChannels(t)

	lp := &Loadpoint{
		log:         util.NewLogger("foo"),
		bus:         evbus.New(),
		clock:       clk,
		charger:     charger,
		chargeMeter: &Null{}, // silence nil panics
		chargeRater: &Null{}, // silence nil panics
		chargeTimer: &Null{}, // silence nil panics
		wakeUpTimer: NewTimer(),
		enabled:     true, // already charging
		minCurrent:  minA,
		maxCurrent:  maxA,
		phases:      1,
		status:      api.StatusC,
		mode:        api.ModePV, // not Off, so plan logic is not short-circuited
		planner:     planner.New(util.NewLogger("foo"), trf, planner.WithClock(clk)),
		site:        &Site{tariffs: &tariff.Tariffs{Planner: trf}},
		planTime:    clk.Now().Add(2 * time.Hour),
		planEnergy:  0.6, // kWh => ~10min, only the cheap window is selected
	}
	attachChannels(lp, uiChan, pushChan, lpChan)

	lp.Update(0, 0, nil, nil, false, false, 0, nil, nil, nil)

	assert.True(t, lp.planBridging, "expected planBridging")
}

// hoursFromNow returns an offset relative to the mock clock epoch, resolved by shift().
func hoursFromNow(h int) time.Time {
	return time.Time{}.Add(time.Duration(h) * time.Hour)
}

// shift rebases the relative rate offsets onto the given base time.
func shift(rr api.Rates, base time.Time) api.Rates {
	res := make(api.Rates, len(rr))
	for i, r := range rr {
		res[i] = api.Rate{
			Start: base.Add(r.Start.Sub(time.Time{})),
			End:   base.Add(r.End.Sub(time.Time{})),
			Value: r.Value,
		}
	}
	return res
}
