package onboarding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSM_InitialState(t *testing.T) {
	f := NewFSM(StatePending)
	assert.Equal(t, StatePending, f.State())
}

func TestFSM_ValidTransition(t *testing.T) {
	f := NewFSM(StatePending)
	require.NoError(t, f.Transition(StateAdminSet))
	assert.Equal(t, StateAdminSet, f.State())
}

func TestFSM_FullFlow(t *testing.T) {
	f := NewFSM(StatePending)
	require.NoError(t, f.Transition(StateAdminSet))
	assert.Equal(t, StateAdminSet, f.State())
	require.NoError(t, f.Transition(StateAppKeySet))
	assert.Equal(t, StateAppKeySet, f.State())
	require.NoError(t, f.Transition(StateComplete))
	assert.Equal(t, StateComplete, f.State())
}

func TestFSM_InvalidTransition(t *testing.T) {
	f := NewFSM(StatePending)
	err := f.Transition(StateComplete)
	assert.Error(t, err)
	assert.Equal(t, StatePending, f.State())
}

func TestFSM_InvalidTransitionFromComplete(t *testing.T) {
	f := NewFSM(StatePending)
	require.NoError(t, f.Transition(StateAdminSet))
	require.NoError(t, f.Transition(StateAppKeySet))
	require.NoError(t, f.Transition(StateComplete))
	// Complete → Pending is now valid (reset)
	require.NoError(t, f.Transition(StatePending))
	assert.Equal(t, StatePending, f.State())
}

func TestFSM_Idempotent(t *testing.T) {
	f := NewFSM(StatePending)
	require.NoError(t, f.Transition(StateAdminSet))
	// transitioning to current state is a no-op
	require.NoError(t, f.Transition(StateAdminSet))
	assert.Equal(t, StateAdminSet, f.State())
}

func TestFSM_CanTransition(t *testing.T) {
	f := NewFSM(StatePending)
	assert.True(t, f.CanTransition(StateAdminSet))
	assert.False(t, f.CanTransition(StateAppKeySet))
	assert.True(t, f.CanTransition(StatePending)) // idempotent

	require.NoError(t, f.Transition(StateAdminSet))
	assert.True(t, f.CanTransition(StateAppKeySet))
	assert.False(t, f.CanTransition(StateComplete))
}

func TestFSM_ResetFromComplete(t *testing.T) {
	f := NewFSM(StatePending)
	require.NoError(t, f.Transition(StateAdminSet))
	require.NoError(t, f.Transition(StateAppKeySet))
	require.NoError(t, f.Transition(StateComplete))
	assert.True(t, f.CanTransition(StatePending))
	require.NoError(t, f.Transition(StatePending))
	assert.Equal(t, StatePending, f.State())
}

func TestFSM_ResetFromAppKeySet(t *testing.T) {
	f := NewFSM(StatePending)
	require.NoError(t, f.Transition(StateAdminSet))
	require.NoError(t, f.Transition(StateAppKeySet))
	assert.True(t, f.CanTransition(StatePending))
	require.NoError(t, f.Transition(StatePending))
	assert.Equal(t, StatePending, f.State())
}

func TestFSM_ResetFromAdminSet(t *testing.T) {
	f := NewFSM(StatePending)
	require.NoError(t, f.Transition(StateAdminSet))
	assert.True(t, f.CanTransition(StatePending))
	require.NoError(t, f.Transition(StatePending))
	assert.Equal(t, StatePending, f.State())
}

func TestFSM_InvalidTransitionError(t *testing.T) {
	f := NewFSM(StatePending)
	// Pending → Complete is invalid (must go through AdminSet → AppKeySet → Complete)
	err := f.Transition(StateComplete)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "complete")
}
