package planner

import "github.com/benbjohnson/clock"

// WithClock sets the planner's clock (fork: mockable time for loadpoint plan tests)
func WithClock(clock clock.Clock) func(t *Planner) {
	return func(t *Planner) {
		t.clock = clock
	}
}
