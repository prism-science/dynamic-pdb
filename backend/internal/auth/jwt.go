package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("auth: invalid token")

type JWT struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

type JWTOption func(*JWT)

func WithClock(now func() time.Time) JWTOption {
	return func(j *JWT) { j.now = now }
}

func NewJWT(secret, issuer string, ttl time.Duration, opts ...JWTOption) *JWT {
	j := &JWT{
		secret: []byte(secret),
		issuer: issuer,
		ttl:    ttl,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

func (j *JWT) Issue(userID uuid.UUID) (string, time.Time, error) {
	now := j.now()
	expiresAt := now.Add(j.ttl)

	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		Issuer:    j.issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(j.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign jwt: %w", err)
	}
	return signed, expiresAt, nil
}

func (j *JWT) Parse(tokenString string) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(*jwt.Token) (any, error) { return j.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(j.issuer),
		jwt.WithTimeFunc(j.now),
	)
	if err != nil || !parsed.Valid {
		return uuid.Nil, ErrInvalidToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: parse subject: %w", err)
	}
	return userID, nil
}
