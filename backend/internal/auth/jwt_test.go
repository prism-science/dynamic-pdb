package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/auth"
)

func Test_should_issue_token_with_expected_claims_when_user_id_provided(t *testing.T) {
	// given
	secret := "test-secret"
	issuer := "dynamic-pdb-backend"
	ttl := 30 * 24 * time.Hour
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("7b7c0b87-3a0f-49f8-9c7a-6e86b4b3e7c4")
	j := auth.NewJWT(secret, issuer, ttl, auth.WithClock(func() time.Time { return now }))

	// when
	token, expiresAt, err := j.Issue(userID)

	// then
	require.NoError(t, err)
	assert.Equal(t, now.Add(ttl), expiresAt)

	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithTimeFunc(func() time.Time { return now }))
	require.NoError(t, err)

	claims := parsed.Claims.(*jwt.RegisteredClaims)
	assert.Equal(t, userID.String(), claims.Subject)
	assert.Equal(t, issuer, claims.Issuer)
	assert.Equal(t, now.Unix(), claims.IssuedAt.Unix())
	assert.Equal(t, now.Add(ttl).Unix(), claims.ExpiresAt.Unix())
}

func Test_should_reject_token_when_verified_with_different_secret(t *testing.T) {
	// given
	j := auth.NewJWT("right-secret", "dynamic-pdb-backend", time.Hour)
	token, _, err := j.Issue(uuid.New())
	require.NoError(t, err)

	// when
	_, err = jwt.Parse(token, func(*jwt.Token) (any, error) {
		return []byte("wrong-secret"), nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	// then
	assert.Error(t, err)
}

func Test_should_return_user_id_when_parse_called_with_token_issued_by_same_secret(t *testing.T) {
	// given
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("7b7c0b87-3a0f-49f8-9c7a-6e86b4b3e7c4")
	j := auth.NewJWT("secret", "dynamic-pdb-backend", time.Hour, auth.WithClock(func() time.Time { return now }))
	token, _, err := j.Issue(userID)
	require.NoError(t, err)

	// when
	parsed, err := j.Parse(token)

	// then
	require.NoError(t, err)
	assert.Equal(t, userID, parsed)
}

func Test_should_return_invalid_token_when_parse_called_with_different_secret(t *testing.T) {
	// given
	issuer := auth.NewJWT("right-secret", "dynamic-pdb-backend", time.Hour)
	token, _, err := issuer.Issue(uuid.New())
	require.NoError(t, err)
	verifier := auth.NewJWT("wrong-secret", "dynamic-pdb-backend", time.Hour)

	// when
	_, err = verifier.Parse(token)

	// then
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func Test_should_return_invalid_token_when_parse_called_after_expiration(t *testing.T) {
	// given
	issuedAt := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	expired := issuedAt.Add(2 * time.Hour)
	j := auth.NewJWT("secret", "dynamic-pdb-backend", time.Hour, auth.WithClock(func() time.Time { return issuedAt }))
	token, _, err := j.Issue(uuid.New())
	require.NoError(t, err)
	j = auth.NewJWT("secret", "dynamic-pdb-backend", time.Hour, auth.WithClock(func() time.Time { return expired }))

	// when
	_, err = j.Parse(token)

	// then
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func Test_should_return_invalid_token_when_parse_called_with_wrong_issuer(t *testing.T) {
	// given
	issuer := auth.NewJWT("secret", "right-issuer", time.Hour)
	token, _, err := issuer.Issue(uuid.New())
	require.NoError(t, err)
	verifier := auth.NewJWT("secret", "wrong-issuer", time.Hour)

	// when
	_, err = verifier.Parse(token)

	// then
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func Test_should_use_real_clock_when_no_clock_option_set(t *testing.T) {
	// given
	ttl := time.Hour
	j := auth.NewJWT("s", "dynamic-pdb-backend", ttl)

	// when
	before := time.Now()
	_, expiresAt, err := j.Issue(uuid.New())
	after := time.Now()

	// then
	require.NoError(t, err)
	assert.False(t, expiresAt.Before(before.Add(ttl)))
	assert.False(t, expiresAt.After(after.Add(ttl)))
}
