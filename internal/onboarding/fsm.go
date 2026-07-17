package onboarding

import "sync"

// StateMachine is the interface for the onboarding FSM.
// Implemented by FSM for production and testable in isolation.
type StateMachine interface {
	State() OnboardingState
	CanTransition(target OnboardingState) bool
	Transition(target OnboardingState) error
	SetState(s OnboardingState)
}

// FSM is a minimal finite state machine for onboarding.
// It owns the current state in memory and validates transitions
// against a static transition table. All methods are safe for
// concurrent use.
type FSM struct {
	mu          sync.Mutex
	state       OnboardingState
	transitions map[OnboardingState]map[OnboardingState]bool
}

// NewFSM creates an FSM in the given initial state with the standard
// onboarding transition table.
func NewFSM(initial OnboardingState) *FSM {
	return &FSM{
		state: initial,
		transitions: map[OnboardingState]map[OnboardingState]bool{
			StatePending:   {StateAdminSet: true},
			StateAdminSet:  {StateAppKeySet: true, StatePending: true},
			StateAppKeySet: {StateComplete: true, StatePending: true},
			StateComplete:  {StatePending: true},
		},
	}
}

// State returns the current state.
func (f *FSM) State() OnboardingState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

// CanTransition reports whether a transition to target is valid.
func (f *FSM) CanTransition(target OnboardingState) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state == target {
		return true
	}
	allowed, ok := f.transitions[f.state]
	return ok && allowed[target]
}

// Transition moves to target. Returns an error if the transition is invalid.
// Idempotent if already in the target state.
func (f *FSM) Transition(target OnboardingState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state == target {
		return nil
	}
	if !f.canTransitionLocked(target) {
		return errInvalidTransition{from: f.state, to: target}
	}
	f.state = target
	return nil
}

// canTransitionLocked is the lock-free internal helper for CanTransition.
// Caller must hold f.mu.
func (f *FSM) canTransitionLocked(target OnboardingState) bool {
	if f.state == target {
		return true
	}
	allowed, ok := f.transitions[f.state]
	return ok && allowed[target]
}

// SetState forces the FSM to the given state without transition validation.
// Used for rollback after a persist failure where the validated reverse
// transition may not exist in the transition table.
func (f *FSM) SetState(s OnboardingState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
}

// errInvalidTransition is returned when a state transition is not allowed.
type errInvalidTransition struct {
	from OnboardingState
	to   OnboardingState
}

func (e errInvalidTransition) Error() string {
	return "invalid onboarding transition: " + string(e.from) + " → " + string(e.to)
}
