package onboarding

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFSM_ConcurrentAccess verifies that the FSM is safe for concurrent use.
// Regression for Kody finding: FSM methods were not protected by a mutex,
// causing data races between HTTP handlers and the background init goroutine.
func TestFSM_ConcurrentAccess(t *testing.T) {
	fsm := NewFSM(StateAppKeySet)

	var wg sync.WaitGroup
	const goroutines = 50

	// Half the goroutines read state
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fsm.State()
			_ = fsm.CanTransition(StateComplete)
		}()
	}

	// Half transition back and forth
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fsm.Transition(StateComplete)
			_ = fsm.Transition(StateAppKeySet)
		}()
	}

	wg.Wait()

	// Final state should be valid (either AppKeySet or Complete)
	finalState := fsm.State()
	assert.Contains(t, []OnboardingState{StateAppKeySet, StateComplete}, finalState)
}

// TestFSM_SetState_OverridesTransition verifies that SetState forces the FSM
// to a given state without transition validation. This is the mechanism used
// by the onInitFailure callback to roll back to StateAppKeySet.
// Regression for Kody finding: FSM/store state desync on init failure.
func TestFSM_SetState_OverridesTransition(t *testing.T) {
	fsm := NewFSM(StateComplete)

	// StateComplete cannot normally transition to StateAppKeySet
	// (the transition table only allows Complete → Pending)
	assert.False(t, fsm.CanTransition(StateAppKeySet))

	// But SetState bypasses the transition table
	fsm.SetState(StateAppKeySet)
	assert.Equal(t, StateAppKeySet, fsm.State())
}
