package httpapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
)

func Test_should_return_user_when_user_was_stored_in_context(t *testing.T) {
	// given
	user := &models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "github", Value: "42"},
		Email:       "user@example.com",
	}
	ctx := WithUser(context.Background(), user)

	// when
	got, ok := UserFromContext(ctx)

	// then
	assert.True(t, ok)
	assert.Equal(t, user, got)
}

func Test_should_return_false_when_context_has_no_user(t *testing.T) {
	// given
	ctx := context.Background()

	// when
	got, ok := UserFromContext(ctx)

	// then
	assert.False(t, ok)
	assert.Nil(t, got)
}
