package views

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.lumeweb.com/s3-server/internal/views/components"
)

func TestUserSelectOptions_Empty(t *testing.T) {
	result := UserSelectOptions([]UserInfo{})
	assert.Empty(t, result)
}

func TestUserSelectOptions_Nil(t *testing.T) {
	result := UserSelectOptions(nil)
	assert.Empty(t, result)
}

func TestUserSelectOptions_Single(t *testing.T) {
	result := UserSelectOptions([]UserInfo{{Name: "admin", KeyCount: 1}})
	assert.Len(t, result, 1)
	assert.Equal(t, components.SelectOption{Value: "admin", Label: "admin"}, result[0])
}

func TestUserSelectOptions_Multiple(t *testing.T) {
	result := UserSelectOptions([]UserInfo{
		{Name: "admin", KeyCount: 1},
		{Name: "ops-team", KeyCount: 2},
		{Name: "ci-user", KeyCount: 1},
	})
	assert.Len(t, result, 3)
	assert.Equal(t, "admin", result[0].Value)
	assert.Equal(t, "admin", result[0].Label)
	assert.Equal(t, "ops-team", result[1].Value)
	assert.Equal(t, "ops-team", result[1].Label)
	assert.Equal(t, "ci-user", result[2].Value)
	assert.Equal(t, "ci-user", result[2].Label)
}

func TestUserSelectOptions_ValueEqualsLabel(t *testing.T) {
	users := []UserInfo{
		{Name: "alice", KeyCount: 1},
		{Name: "bob", KeyCount: 1},
		{Name: "charlie", KeyCount: 1},
	}
	result := UserSelectOptions(users)
	for i, u := range users {
		assert.Equal(t, u.Name, result[i].Value)
		assert.Equal(t, u.Name, result[i].Label)
	}
}

func TestUserSelectOptions_FiltersKeylessUsers(t *testing.T) {
	result := UserSelectOptions([]UserInfo{
		{Name: "admin", KeyCount: 2},
		{Name: "keyless", KeyCount: 0},
		{Name: "ops", KeyCount: 1},
	})
	assert.Len(t, result, 2)
	assert.Equal(t, "admin", result[0].Value)
	assert.Equal(t, "ops", result[1].Value)
}

func TestUserSelectOptions_AllKeyless(t *testing.T) {
	result := UserSelectOptions([]UserInfo{
		{Name: "admin", KeyCount: 0},
		{Name: "ops", KeyCount: 0},
	})
	assert.Empty(t, result)
}
