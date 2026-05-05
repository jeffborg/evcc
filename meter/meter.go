package meter

import (
	"context"
	"fmt"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/meter/measurement"
	"github.com/evcc-io/evcc/plugin"
	"github.com/evcc-io/evcc/util"
)

func init() {
	registry.AddCtx(api.Custom, NewConfigurableFromConfig)
}

//go:generate go tool decorate

//evcc:function decorateMeter
//evcc:basetype api.Meter
//evcc:types api.MeterEnergy,api.PhaseCurrents,api.PhaseVoltages,api.PhasePowers,api.MaxACPowerGetter

//evcc:function decorateMeterBattery
//evcc:basetype api.Meter
//evcc:types api.MeterEnergy,api.Battery,api.BatteryCapacity,api.BatterySocLimiter,api.BatteryPowerLimiter,api.BatteryController

// NewConfigurableFromConfig creates a new meter from config
func NewConfigurableFromConfig(ctx context.Context, other map[string]any) (api.Meter, error) {
	cc := struct {
		measurement.Energy `mapstructure:",squash"` // energy optional
		measurement.Phases `mapstructure:",squash"` // optional

		// pv
		pvMaxACPower `mapstructure:",squash"`

		// battery
		batteryCapacity    `mapstructure:",squash"`
		batteryPowerLimits `mapstructure:",squash"`
		MinSoc             any            `mapstructure:"minsoc"` // float or plugin config
		MaxSoc             any            `mapstructure:"maxsoc"` // float or plugin config
		Soc                *plugin.Config // optional
		LimitSoc           *plugin.Config // optional
		BatteryMode        *plugin.Config // optional
	}{}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	powerG, energyG, err := cc.Energy.Configure(ctx)
	if err != nil {
		return nil, err
	}

	currentsG, voltagesG, powersG, err := cc.Phases.Configure(ctx)
	if err != nil {
		return nil, err
	}

	m, _ := NewConfigurable(powerG)

	// decorate soc
	socG, err := cc.Soc.FloatGetter(ctx)
	if err != nil {
		return nil, fmt.Errorf("battery soc: %w", err)
	}

	// create soc limit getters (supporting both static values and plugins)
	minSocG, err := floatOrPluginGetter(ctx, cc.MinSoc, 20)
	if err != nil {
		return nil, fmt.Errorf("battery minsoc: %w", err)
	}

	maxSocG, err := floatOrPluginGetter(ctx, cc.MaxSoc, 95)
	if err != nil {
		return nil, fmt.Errorf("battery maxsoc: %w", err)
	}

	var batModeS func(api.BatteryMode) error

	switch {
	case cc.Soc != nil && cc.LimitSoc != nil:
		limitSocS, err := cc.LimitSoc.FloatSetter(ctx, "limitSoc")
		if err != nil {
			return nil, fmt.Errorf("battery limit soc: %w", err)
		}

		batModeS = socLimitController(minSocG, maxSocG, socG, limitSocS)

	case cc.BatteryMode != nil:
		modeS, err := cc.BatteryMode.IntSetter(ctx, "batteryMode")
		if err != nil {
			return nil, fmt.Errorf("battery mode: %w", err)
		}

		batModeS = func(mode api.BatteryMode) error {
			return modeS(int64(mode))
		}
	}

	if socG != nil {
		socLimitsDecorator := func() (float64, float64) {
			minSoc, err := minSocG()
			if err != nil {
				minSoc = 20
			}
			maxSoc, err := maxSocG()
			if err != nil {
				maxSoc = 95
			}
			return minSoc, maxSoc
		}

		return m.DecorateBattery(
			energyG,
			socG, cc.batteryCapacity.Decorator(),
			socLimitsDecorator, cc.batteryPowerLimits.Decorator(),
			batModeS,
		), nil
	}

	return m.Decorate(
		energyG, currentsG, voltagesG, powersG, cc.pvMaxACPower.Decorator(),
	), nil
}

// floatOrPluginGetter creates a float getter from either a static value or a plugin configuration.
// If val is nil, the default value is used and the returned getter always returns that constant.
// If val is a numeric type (int or float64), it is used as a static constant.
// If val is a map, it is decoded as a plugin.Config and the plugin's FloatGetter is used;
// in this case the returned getter may be called multiple times and may return different values over time.
func floatOrPluginGetter(ctx context.Context, val any, def float64) (func() (float64, error), error) {
	switch v := val.(type) {
	case nil:
		return func() (float64, error) { return def, nil }, nil
	case int:
		f := float64(v)
		return func() (float64, error) { return f, nil }, nil
	case float64:
		return func() (float64, error) { return v, nil }, nil
	case map[string]any:
		var cfg plugin.Config
		if err := util.DecodeOther(v, &cfg); err != nil {
			return nil, err
		}
		return cfg.FloatGetter(ctx)
	default:
		return nil, fmt.Errorf("unsupported type %T, expected number or plugin config", val)
	}
}

// socLimitController returns a battery mode setter that dynamically retrieves soc limits on each
// invocation using getter functions rather than static values, enabling plugin-based soc limits.
func socLimitController(minSocG, maxSocG func() (float64, error), socG func() (float64, error), limitSocS func(float64) error) func(api.BatteryMode) error {
	return func(mode api.BatteryMode) error {
		switch mode {
		case api.BatteryNormal:
			minSoc, err := minSocG()
			if err != nil {
				return err
			}
			return limitSocS(minSoc)

		case api.BatteryHold:
			soc, err := socG()
			if err != nil {
				return err
			}
			minSoc, err := minSocG()
			if err != nil {
				return err
			}
			return limitSocS(min(100, max(soc, minSoc)))

		case api.BatteryCharge:
			maxSoc, err := maxSocG()
			if err != nil {
				return err
			}
			return limitSocS(maxSoc)

		default:
			return api.ErrNotAvailable
		}
	}
}

// NewConfigurable creates a new meter
func NewConfigurable(currentPowerG func() (float64, error)) (*Meter, error) {
	m := &Meter{
		currentPowerG: currentPowerG,
	}
	return m, nil
}

// Meter is an api.Meter implementation with configurable getters and setters.
type Meter struct {
	currentPowerG func() (float64, error)
}

// Decorate attaches additional capabilities to the base meter
func (m *Meter) Decorate(
	totalEnergy func() (float64, error),
	currents, voltages, powers func() (float64, float64, float64, error),
	maxACPower func() float64,
) api.Meter {
	return decorateMeter(m,
		totalEnergy, currents, voltages, powers,
		maxACPower,
	)
}

func (m *Meter) DecorateBattery(
	totalEnergy func() (float64, error),
	soc func() (float64, error), capacity func() float64,
	socLimits, powerLimits func() (float64, float64),
	setMode func(api.BatteryMode) error,
) api.Meter {
	return decorateMeterBattery(m,
		totalEnergy,
		soc, capacity,
		socLimits, powerLimits,
		setMode,
	)
}

// CurrentPower implements the api.Meter interface
func (m *Meter) CurrentPower() (float64, error) {
	return m.currentPowerG()
}
