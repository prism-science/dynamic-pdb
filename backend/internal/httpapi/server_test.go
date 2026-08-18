package httpapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_identify_admin_when_user_id_is_in_admin_user_ids(t *testing.T) {
	// given
	adminUserID := uuid.MustParse("8ca59596-c4b4-4f3f-94be-6dd73f76f050")
	otherAdminUserID := uuid.MustParse("5a8ed753-5eb0-483f-b9f8-a8c58b191017")
	server := NewServer(nil, nil, auth.Config{
		AdminUserIDs: []string{
			adminUserID.String(),
			otherAdminUserID.String(),
		},
	}, nil, nil)

	// when
	got := server.isAdmin(&models.User{ID: otherAdminUserID})

	// then
	assert.True(t, got)
}

func Test_should_not_identify_admin_when_user_id_is_not_configured(t *testing.T) {
	// given
	server := NewServer(nil, nil, auth.Config{
		AdminUserIDs: []string{
			"8ca59596-c4b4-4f3f-94be-6dd73f76f050",
		},
	}, nil, nil)

	// when
	got := server.isAdmin(&models.User{ID: uuid.MustParse("5a8ed753-5eb0-483f-b9f8-a8c58b191017")})

	// then
	assert.False(t, got)
}
