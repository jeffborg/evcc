package core

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/soc"
	"github.com/evcc-io/evcc/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPlanRequiredDurationVehicleRemoved(t *testing.T) {
	ctrl := gomock.NewController(t)

	lp := NewLoadpoint(util.NewLogger("foo"), nil)
	lp.vehicleSoc = 50

	// remove the vehicle while socBasedPlanning reads its capacity, i.e. before the snapshot
	vehicle := api.NewMockVehicle(ctrl)
	vehicle.EXPECT().Capacity().DoAndReturn(func() float64 {
		lp.vmu.Lock()
		lp.vehicle = nil
		lp.vmu.Unlock()
		return 50
	})
	lp.vehicle = vehicle

	assert.Equal(t, time.Duration(0), lp.GetPlanRequiredDuration(80, 10e3))
}

func newUnsampledSocLoadpoint(t *testing.T) *Loadpoint {
	ctrl := gomock.NewController(t)

	// 8.5 kWh capacity => 10 kWh virtual capacity at 85% efficiency
	vehicle := api.NewMockVehicle(ctrl)
	vehicle.EXPECT().Capacity().Return(8.5).AnyTimes()
	vehicle.EXPECT().Features().AnyTimes()

	log := util.NewLogger("foo")
	return &Loadpoint{
		log:          log,
		vehicle:      vehicle,
		status:       api.StatusC,
		vehicleSoc:   40, // stale SoC from a previous session
		socEstimator: soc.NewEstimator(log, vehicle),
	}
}

func TestPlanRequiredDurationUnsampledSoc(t *testing.T) {
	lp := newUnsampledSocLoadpoint(t)

	for _, tc := range []struct {
		charged  float64 // kWh
		expected time.Duration
	}{
		{0, 3*time.Hour + 30*time.Minute},
		{2, time.Hour + 30*time.Minute},
		{3.5, 0},
		{25, 0},
	} {
		lp.energyMetrics.totalKWh = tc.charged
		assert.Equal(t, tc.expected, lp.getPlanRequiredDuration(35, 1000), "charged %.1fkWh", tc.charged)
	}

	// energy of a previous session doesn't count while disconnected
	lp.status = api.StatusA
	assert.Equal(t, 3*time.Hour+30*time.Minute, lp.getPlanRequiredDuration(35, 1000))

	// a valid SoC takes over
	lp.status = api.StatusC
	lp.energyMetrics.totalKWh = 25
	vehicleSoc := 30.0
	lp.socEstimator.Soc(&vehicleSoc, lp.getChargedEnergy())
	assert.Equal(t, 30*time.Minute, lp.getPlanRequiredDuration(35, 1000))
}

func TestNextActivePlanUnsampledSoc(t *testing.T) {
	lp := newUnsampledSocLoadpoint(t)
	plans := []plan{{Id: 2, Soc: 35, End: time.Now().Add(12 * time.Hour)}}

	// stale SoC above the goal must not skip the plan
	p := lp.nextActivePlan(1000, plans)
	require.NotNil(t, p)
	assert.Equal(t, 2, p.Id)

	// goal energy charged
	lp.energyMetrics.totalKWh = 3.5
	assert.Nil(t, lp.nextActivePlan(1000, plans))
}

// TestPlannerRateGap verifies detection of an undefined-tariff window (a demand
// window left undefined), both interior and leading (forward-pruned rates), vs.
// covered slots, static/no tariff and the region beyond the published horizon.
func TestPlannerRateGap(t *testing.T) {
	base := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return base.Add(time.Duration(h) * time.Hour) }

	// rates cover 08:00-15:00 and 21:00-24:00, leaving 15:00-21:00 as a gap;
	// includes a zero-price slot to prove a gap differs from a 0 value
	rates := api.Rates{
		{Start: at(8), End: at(12), Value: 0.20},
		{Start: at(12), End: at(15), Value: 0}, // zero-price, but covered
		{Start: at(21), End: at(24), Value: 0.10},
	}

	tc := []struct {
		name  string
		rates api.Rates
		t     time.Time
		want  bool
	}{
		{"interior gap (demand window)", rates, at(18), true},
		{"covered slot", rates, at(9), false},
		{"covered zero-price slot", rates, at(13), false},
		{"gap edge start", rates, at(15), true}, // 15:00 not covered (slot ends at 15:00)
		{"gap edge end", rates, at(20), true},   // still before 21:00 slot
		// planner rates are forward-pruned, so a t before the first slot means the
		// current period is undefined (a leading demand-window gap) - treat as gap
		{"leading gap (undefined now)", rates, at(7), true},
		{"beyond horizon tail", rates, at(24), false}, // at/after last slot end
		{"no rates", nil, at(18), false},
		{"empty rates", api.Rates{}, at(18), false},
	}

	for _, tc := range tc {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, plannerRateGap(tc.rates, tc.t))
		})
	}
}
