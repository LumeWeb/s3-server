package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCls_EmptyArgs(t *testing.T) {
	assert.Equal(t, "", Cls())
}

func TestCls_AllEmpty(t *testing.T) {
	assert.Equal(t, "", Cls("", "", ""))
}

func TestCls_SingleString(t *testing.T) {
	assert.Equal(t, "btn-primary", Cls("btn-primary"))
}

func TestCls_MultipleStrings(t *testing.T) {
	assert.Equal(t, "btn-primary text-sm font-bold", Cls("btn-primary", "text-sm", "font-bold"))
}

func TestCls_SkipsEmptyStrings(t *testing.T) {
	assert.Equal(t, "btn-primary text-sm", Cls("btn-primary", "", "text-sm", ""))
}

func TestCls_BooleanFalse(t *testing.T) {
	// false is skipped, but other strings still pass through
	assert.Equal(t, "btn-primary text-sm", Cls("btn-primary", false, "text-sm"))
}

func TestCls_BooleanTrue(t *testing.T) {
	// true is skipped (used as conditional guard by caller)
	assert.Equal(t, "btn-primary text-sm", Cls("btn-primary", true, "text-sm"))
}

func TestCls_ConditionalPatternTrue(t *testing.T) {
	// Simulates templ conditional: isActive ? "font-bold" : ""
	isActive := true
	cls := "btn-primary"
	if isActive {
		cls += " font-bold"
	}
	result := Cls(cls)
	assert.Equal(t, "btn-primary font-bold", result)
}

func TestCls_ConditionalPatternFalse(t *testing.T) {
	isActive := false
	cls := "btn-primary"
	if isActive {
		cls += " font-bold"
	}
	result := Cls(cls)
	assert.Equal(t, "btn-primary", result)
}

func TestCls_IntegerArg(t *testing.T) {
	// Non-string types are stringified via fmt.Sprint, "false" filtered out
	// 0 stringifies to "0" which is not "false" or "", so it passes through
	assert.Equal(t, "btn-primary 0", Cls("btn-primary", 0))
}

func TestCls_NonZeroInt(t *testing.T) {
	assert.Equal(t, "btn-primary 42", Cls("btn-primary", 42))
}

func TestCls_StringFalseNotFiltered(t *testing.T) {
	// The string "false" is only filtered for non-string types via fmt.Sprint;
	// a literal string "false" passes through like any other string
	assert.Equal(t, "btn-primary false", Cls("btn-primary", "false"))
}

func TestCls_MixedTypes(t *testing.T) {
	result := Cls("btn", "active", "text-sm", false, 42)
	assert.Equal(t, "btn active text-sm 42", result)
}

func TestCls_SingleBooleanFalse(t *testing.T) {
	assert.Equal(t, "", Cls(false))
}

func TestCls_SingleBooleanTrue(t *testing.T) {
	assert.Equal(t, "", Cls(true))
}

func TestCls_NilArg(t *testing.T) {
	// nil stringifies to "<nil>", which is not "" or "false", so it passes through
	result := Cls(nil)
	assert.Equal(t, "<nil>", result)
}

func TestCls_OnlyBooleans(t *testing.T) {
	assert.Equal(t, "", Cls(true, false, true))
}
