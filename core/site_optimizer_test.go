package core

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/core/types"
	"github.com/evcc-io/evcc/tariff"
	"github.com/evcc-io/evcc/util/config"
	optimizer "github.com/evcc-io/optimizer/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestLoadpointProfile(t *testing.T) {
	ctrl := gomock.NewController(t)

	lp := loadpoint.NewMockAPI(ctrl)
	lp.EXPECT().GetMode().Return(api.ModeMinPV).AnyTimes()
	lp.EXPECT().GetStatus().Return(api.StatusC).AnyTimes()
	lp.EXPECT().GetChargePower().Return(10000.0).AnyTimes()   //  10 kW
	lp.EXPECT().EffectiveMinPower().Return(1000.0).AnyTimes() //   1 kW
	lp.EXPECT().GetRemainingEnergy().Return(1.8).AnyTimes()   // 1.8 kWh

	// expected slots: 0.25 kWh...
	require.Equal(t, []float64{250, 250, 250, 250, 250, 250, 250, 50}, loadpointProfile(lp, 8))
}

func TestAsTimestamps(t *testing.T) {
	// now is 10 minutes into a 15-minute slot
	now := time.Date(2025, 1, 1, 12, 10, 0, 0, time.UTC)

	// dt[0]=300 means first event is 300s (5min) before end of current slot
	// dt[1..] just mark subsequent slot boundaries
	dt := []int{60 * 5, 60 * 15, 60 * 15}

	got := asTimestamps(dt, now)

	// current slot: 12:00–12:15
	// first timestamp: 12:15 - 5min = 12:10
	// subsequent: 12:15, 12:30
	assert.Equal(t, []time.Time{
		time.Date(2025, 1, 1, 12, 10, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 12, 15, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 12, 30, 0, 0, time.UTC),
	}, got)
}

func TestBatteryForecastSocExtremes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		req       []optimizer.BatteryConfig
		soc       [][]float32
		high, low *batteryForecastSlot
	}{
		{
			"no home battery",
			[]optimizer.BatteryConfig{{SMax: 80}}, // SCapacity unset → vehicle
			[][]float32{{1000, 2000}},
			nil, nil,
		},
		{
			"single home battery rising — reaches full",
			[]optimizer.BatteryConfig{{SCapacity: 1000, SMax: 1000}},
			[][]float32{{200, 500, 1000}},
			&batteryForecastSlot{slot: 2, soc: 100, limit: true},
			&batteryForecastSlot{slot: 0, soc: 20, limit: false},
		},
		{
			"single home battery falling — reaches empty",
			[]optimizer.BatteryConfig{{SCapacity: 1000, SMax: 1000}},
			[][]float32{{900, 500, 0}},
			&batteryForecastSlot{slot: 0, soc: 90, limit: false},
			&batteryForecastSlot{slot: 2, soc: 0, limit: true},
		},
		{
			"single home battery — local extremes (no limit reached)",
			[]optimizer.BatteryConfig{{SCapacity: 1000, SMax: 900, SMin: 100}},
			[][]float32{{500, 800, 200}},
			&batteryForecastSlot{slot: 1, soc: 80, limit: false},
			&batteryForecastSlot{slot: 2, soc: 20, limit: false},
		},
		{
			"two home batteries aggregated",
			[]optimizer.BatteryConfig{
				{SCapacity: 1000, SMax: 1000},
				{SCapacity: 1000, SMax: 1000},
			},
			[][]float32{
				{200, 400, 1000},
				{800, 400, 1000},
			},
			&batteryForecastSlot{slot: 2, soc: 100, limit: true},
			&batteryForecastSlot{slot: 1, soc: 40, limit: false},
		},
		{
			"vehicle and home battery — vehicle ignored",
			[]optimizer.BatteryConfig{
				{SMax: 80},                    // vehicle
				{SCapacity: 1000, SMax: 1000}, // home
			},
			[][]float32{
				{0, 0, 80},
				{200, 500, 900},
			},
			&batteryForecastSlot{slot: 2, soc: 90, limit: false},
			&batteryForecastSlot{slot: 0, soc: 20, limit: false},
		},
		{
			"first slot at SMax wins for highest",
			[]optimizer.BatteryConfig{{SCapacity: 1000, SMax: 1000}},
			[][]float32{{1000, 1000, 500}},
			&batteryForecastSlot{slot: 0, soc: 100, limit: true},
			&batteryForecastSlot{slot: 2, soc: 50, limit: false},
		},
		{
			"near SMax is not full",
			[]optimizer.BatteryConfig{{SCapacity: 1000, SMax: 1000}},
			[][]float32{{500, 999, 800}},
			&batteryForecastSlot{slot: 1, soc: 99.9, limit: false},
			&batteryForecastSlot{slot: 0, soc: 50, limit: false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := make([]optimizer.BatteryResult, len(tc.soc))
			for i, s := range tc.soc {
				resp[i] = optimizer.BatteryResult{StateOfCharge: s}
			}

			high, low := batteryForecastSocExtremes(tc.req, resp)

			if tc.high == nil {
				assert.Nil(t, high, "high")
			} else {
				require.NotNil(t, high, "high")
				assert.Equal(t, tc.high.slot, high.slot, "high.slot")
				assert.InDelta(t, tc.high.soc, high.soc, 1e-3, "high.soc")
				assert.Equal(t, tc.high.limit, high.limit, "high.limit")
			}
			if tc.low == nil {
				assert.Nil(t, low, "low")
			} else {
				require.NotNil(t, low, "low")
				assert.Equal(t, tc.low.slot, low.slot, "low.slot")
				assert.InDelta(t, tc.low.soc, low.soc, 1e-3, "low.soc")
				assert.Equal(t, tc.low.limit, low.limit, "low.limit")
			}
		})
	}
}

func TestFillMissingRateSlots(t *testing.T) {
	now := time.Now().Truncate(tariff.SlotDuration)

	rates := api.Rates{
		{Start: now, End: now.Add(tariff.SlotDuration), Value: 1},
		{Start: now.Add(2 * tariff.SlotDuration), End: now.Add(3 * tariff.SlotDuration), Value: 3},
	}

	got := fillMissingRateSlots(rates, 4, plannerRateFallback)

	require.Len(t, got, 4)
	assert.Equal(t, []float64{1, plannerRateFallback, 3, plannerRateFallback}, []float64{
		got[0].Value,
		got[1].Value,
		got[2].Value,
		got[3].Value,
	})
}

func TestRateHorizonSlotsIgnoresMissingPlannerSlots(t *testing.T) {
	now := time.Now().Truncate(tariff.SlotDuration)

	rates := api.Rates{
		{Start: now, End: now.Add(tariff.SlotDuration), Value: 1},
		{Start: now.Add(2 * tariff.SlotDuration), End: now.Add(3 * tariff.SlotDuration), Value: 3},
		{Start: now.Add(95 * tariff.SlotDuration), End: now.Add(96 * tariff.SlotDuration), Value: 96},
	}

	assert.Equal(t, 96, rateHorizonSlots(rates))
}

func TestBatteryRequestDischargeToGrid(t *testing.T) {
	ctrl := gomock.NewController(t)

	site := &Site{optimizerDischargeToGrid: true}
	var meter api.Meter = &struct {
		api.Meter
		api.BatteryController
	}{
		BatteryController: api.NewMockBatteryController(ctrl),
	}
	capacity := 10.0
	soc := 50.0

	bat, _ := site.batteryRequest(config.NewStaticDevice(config.Named{Name: "battery1"}, meter), types.Measurement{
		Soc:      &soc,
		Capacity: &capacity,
	}, nil, 0, 0, nil)

	assert.True(t, bat.DischargeToGrid)
}

func TestOptimizerPA(t *testing.T) {
	t.Run("automatic", func(t *testing.T) {
		site := new(Site)
		assert.InDelta(t, 0.0891, site.optimizerPA([]float32{0.25, 0.10}), 1e-6)
	})

	t.Run("manual override", func(t *testing.T) {
		manual := 0.33
		site := &Site{optimizerManualPA: &manual}
		assert.InDelta(t, 0.00033, site.optimizerPA([]float32{0.25, 0.10}), 1e-9)
	})
}

func TestBatterySocGoalSlots(t *testing.T) {
	loc := time.UTC

	timestamps := []time.Time{
		time.Date(2025, 1, 1, 20, 30, 0, 0, loc),
		time.Date(2025, 1, 1, 20, 45, 0, 0, loc),
		time.Date(2025, 1, 1, 21, 0, 0, 0, loc),
		time.Date(2025, 1, 1, 21, 15, 0, 0, loc),
		time.Date(2025, 1, 2, 20, 45, 0, 0, loc),
		time.Date(2025, 1, 2, 21, 0, 0, 0, loc),
	}

	assert.Equal(t, []float32{0, 0, 2000, 0, 0, 2000}, batterySocGoalSlots(timestamps, loc, 21, 0, 2000))
}

func TestBatterySocGoalSlotsRollsToNextDay(t *testing.T) {
	loc := time.UTC

	timestamps := []time.Time{
		time.Date(2025, 1, 1, 21, 5, 0, 0, loc),
		time.Date(2025, 1, 2, 20, 45, 0, 0, loc),
		time.Date(2025, 1, 2, 21, 15, 0, 0, loc),
	}

	assert.Equal(t, []float32{0, 0, 1500}, batterySocGoalSlots(timestamps, loc, 21, 0, 1500))
}

func TestBatterySocGoalSlotsTimezone(t *testing.T) {
	loc := time.FixedZone("MST", -7*60*60)

	timestamps := []time.Time{
		time.Date(2025, 1, 2, 3, 45, 0, 0, time.UTC),
		time.Date(2025, 1, 2, 4, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 2, 4, 15, 0, 0, time.UTC),
	}

	assert.Equal(t, []float32{0, 2500, 0}, batterySocGoalSlots(timestamps, loc, 21, 0, 2500))
}

func TestBatteryRequestSocGoal(t *testing.T) {
	goal := 20.0
	ctrl := gomock.NewController(t)

	site := &Site{
		batteryOptimizerSocGoal:     &goal,
		batteryOptimizerSocGoalTime: "21:00",
		batteryOptimizerSocGoalTz:   "UTC",
	}
	var meter api.Meter = &struct {
		api.Meter
		api.BatteryController
	}{
		BatteryController: api.NewMockBatteryController(ctrl),
	}
	capacity := 10.0
	soc := 50.0
	timestamps := []time.Time{
		time.Date(2025, 1, 1, 20, 30, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 20, 45, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 21, 0, 0, 0, time.UTC),
	}

	bat, _ := site.batteryRequest(config.NewStaticDevice(config.Named{Name: "battery1"}, meter), types.Measurement{
		Soc:      &soc,
		Capacity: &capacity,
	}, nil, len(timestamps), 0, timestamps)

	assert.Equal(t, []float32{0, 0, 2000}, bat.SGoal)
	assert.Equal(t, float32(10000), bat.SMax)
}

func TestShouldSkipOptimizerUpdate(t *testing.T) {
	now := time.Now()

	assert.True(t, shouldSkipOptimizerUpdate(false, now.Add(-time.Minute), now))
	assert.False(t, shouldSkipOptimizerUpdate(false, now.Add(-3*time.Minute), now))
	assert.False(t, shouldSkipOptimizerUpdate(true, now.Add(-time.Minute), now))
}
