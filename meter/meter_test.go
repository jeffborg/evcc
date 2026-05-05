package meter

import (
	"testing"

	"github.com/evcc-io/evcc/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/evcc-io/evcc/plugin" // register plugins
)

func TestPV(t *testing.T) {
	m, err := NewConfigurableFromConfig(t.Context(), map[string]any{
		"power": map[string]any{
			"source": "const",
			"value":  1000,
		},
		"maxacpower": 1000,
	})
	require.NoError(t, err)

	// must not have soc/capacity
	_, ok := api.Cap[api.MaxACPowerGetter](m)
	assert.True(t, ok, "MaxACPowerGetter")
}
func TestBattery(t *testing.T) {
	m, err := NewConfigurableFromConfig(t.Context(), map[string]any{
		"power": map[string]any{
			"source": "const",
			"value":  1000,
		},
		"capacity": 23,
		"soc": map[string]any{
			"source": "const",
			"value":  47,
		},
	})
	require.NoError(t, err)

	_, ok := api.Cap[api.Battery](m)
	assert.True(t, ok, "Battery")
	_, ok = api.Cap[api.BatteryCapacity](m)
	assert.True(t, ok, "BatteryCapacity")
}

func TestBatterySocLimitsStaticValues(t *testing.T) {
	m, err := NewConfigurableFromConfig(t.Context(), map[string]any{
		"power": map[string]any{
			"source": "const",
			"value":  1000,
		},
		"soc": map[string]any{
			"source": "const",
			"value":  47,
		},
		"minsoc": 10,
		"maxsoc": 90,
	})
	require.NoError(t, err)

	limiter, ok := api.Cap[api.BatterySocLimiter](m)
	require.True(t, ok, "BatterySocLimiter")

	minSoc, maxSoc := limiter.GetSocLimits()
	assert.Equal(t, 10.0, minSoc)
	assert.Equal(t, 90.0, maxSoc)
}

func TestBatterySocLimitsPluginConfig(t *testing.T) {
	m, err := NewConfigurableFromConfig(t.Context(), map[string]any{
		"power": map[string]any{
			"source": "const",
			"value":  1000,
		},
		"soc": map[string]any{
			"source": "const",
			"value":  47,
		},
		"minsoc": map[string]any{
			"source": "const",
			"value":  "15",
		},
		"maxsoc": map[string]any{
			"source": "const",
			"value":  "85",
		},
	})
	require.NoError(t, err)

	limiter, ok := api.Cap[api.BatterySocLimiter](m)
	require.True(t, ok, "BatterySocLimiter")

	minSoc, maxSoc := limiter.GetSocLimits()
	assert.Equal(t, 15.0, minSoc)
	assert.Equal(t, 85.0, maxSoc)
}

func TestBatterySocLimitsDefaults(t *testing.T) {
	m, err := NewConfigurableFromConfig(t.Context(), map[string]any{
		"power": map[string]any{
			"source": "const",
			"value":  1000,
		},
		"soc": map[string]any{
			"source": "const",
			"value":  47,
		},
	})
	require.NoError(t, err)

	limiter, ok := api.Cap[api.BatterySocLimiter](m)
	require.True(t, ok, "BatterySocLimiter")

	minSoc, maxSoc := limiter.GetSocLimits()
	assert.Equal(t, 20.0, minSoc)
	assert.Equal(t, 95.0, maxSoc)
}
