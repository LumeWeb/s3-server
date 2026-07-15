package views

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.lumeweb.com/s3-server/internal/views/components"
)

func TestUserSelectOptions_Empty(t *testing.T) {
	result := UserSelectOptions([]string{})
	assert.Empty(t, result)
}

func TestUserSelectOptions_Nil(t *testing.T) {
	result := UserSelectOptions(nil)
	assert.Empty(t, result)
}

func TestUserSelectOptions_Single(t *testing.T) {
	result := UserSelectOptions([]string{"admin"})
	assert.Len(t, result, 1)
	assert.Equal(t, components.SelectOption{Value: "admin", Label: "admin"}, result[0])
}

func TestUserSelectOptions_Multiple(t *testing.T) {
	result := UserSelectOptions([]string{"admin", "ops-team", "ci-user"})
	assert.Len(t, result, 3)
	assert.Equal(t, "admin", result[0].Value)
	assert.Equal(t, "admin", result[0].Label)
	assert.Equal(t, "ops-team", result[1].Value)
	assert.Equal(t, "ops-team", result[1].Label)
	assert.Equal(t, "ci-user", result[2].Value)
	assert.Equal(t, "ci-user", result[2].Label)
}

func TestUserSelectOptions_ValueEqualsLabel(t *testing.T) {
	// For user dropdowns, value and label are the same (username)
	users := []string{"alice", "bob", "charlie"}
	result := UserSelectOptions(users)
	for i, u := range users {
		assert.Equal(t, u, result[i].Value)
		assert.Equal(t, u, result[i].Label)
	}
}
