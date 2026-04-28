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

func TestBatteryForecastTotals(t *testing.T) {
	site := new(Site)

	req := []optimizer.BatteryConfig{
		{SMax: 80},
		{SMax: 80},
	}

	const zero = -1

	for _, tc := range []struct {
		name        string
		bat1, bat2  []float32
		full, empty int
	}{
		{
			"never full",
			[]float32{0, 0},
			[]float32{0, 0},
			zero, 0,
		},
		{
			"never empty",
			[]float32{100, 100},
			[]float32{100, 100},
			0, zero,
		},
		{
			"first full then empty",
			[]float32{100, 0},
			[]float32{100, 0},
			0, 1,
		},
		{
			"first full finally empty",
			[]float32{100, 100, 0},
			[]float32{100, 0, 0},
			0, 2,
		},
		{
			"first empty then full",
			[]float32{0, 100},
			[]float32{0, 100},
			1, 0,
		},
		{
			"first empty finally full",
			[]float32{0, 100, 100},
			[]float32{0, 0, 100},
			2, 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := []optimizer.BatteryResult{
				{StateOfCharge: tc.bat1},
				{StateOfCharge: tc.bat2},
			}

			full, empty := site.batteryForecastFullAndEmptySlots(req, resp)
			assert.Equal(t, tc.full, full, "full")
			assert.Equal(t, tc.empty, empty, "empty")
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
	})

	assert.True(t, bat.DischargeToGrid)
}
