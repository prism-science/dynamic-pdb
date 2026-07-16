package e2etest

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/integrations/github"
)

type AuthSuite struct {
	baseSuite
}

func TestAuth(t *testing.T) {
	suite.Run(t, new(AuthSuite))
}

func (s *AuthSuite) Test_should_return_token_when_github_user_belongs_to_allowed_org() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 42, Login: "octocat", Name: "Octo Cat", Email: "octo@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	// when
	resp := ExchangeGithubToken(s.T(), "gh-token")
	defer resp.Body.Close()

	// then
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body httpapi.TokenResponse
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(httpapi.Bearer, body.TokenType)
	s.NotEmpty(body.AccessToken)
	s.True(body.ExpiresAt.After(time.Now()))
	s.Equal("Octo Cat", body.Name)
	s.Equal("octo@example.com", body.Email)
	s.Equal("octocat", body.Login)

	parsed, err := jwt.ParseWithClaims(body.AccessToken, &jwt.RegisteredClaims{}, func(*jwt.Token) (any, error) {
		return []byte(testJWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	s.Require().NoError(err)

	claims := parsed.Claims.(*jwt.RegisteredClaims)
	s.Equal(testJWTIssuer, claims.Issuer)
	_, err = uuid.Parse(claims.Subject)
	s.NoError(err, "subject should be a valid UUID")
}

func (s *AuthSuite) Test_should_return_token_when_github_code_exchange_succeeds() {
	// given
	githubClient.On("ExchangeCode", mock.Anything, "code-123", "https://example.com/auth/github/callback").
		Return("gh-token", nil)
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 42, Login: "octocat", Name: "Octo Cat", Email: "octo@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	// when
	resp := ExchangeGithubCode(s.T(), "code-123", "https://example.com/auth/github/callback")
	defer resp.Body.Close()

	// then
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body httpapi.TokenResponse
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(httpapi.Bearer, body.TokenType)
	s.NotEmpty(body.AccessToken)
	s.True(body.ExpiresAt.After(time.Now()))
	s.Equal("Octo Cat", body.Name)
	s.Equal("octo@example.com", body.Email)
	s.Equal("octocat", body.Login)
}

func (s *AuthSuite) Test_should_return_same_subject_when_github_exchange_called_twice_for_same_account() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 42, Login: "octocat"}, nil).Twice()
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil).Twice()

	// when
	//nolint:bodyclose // subjectOf closes the response body via defer.
	first := subjectOf(s.T(), ExchangeGithubToken(s.T(), "gh-token"))
	//nolint:bodyclose // subjectOf closes the response body via defer.
	second := subjectOf(s.T(), ExchangeGithubToken(s.T(), "gh-token"))

	// then
	s.Equal(first, second)
}

func (s *AuthSuite) Test_should_return_401_when_github_token_is_invalid() {
	// given
	githubClient.On("GetUser", mock.Anything, "bad-token").
		Return(github.User{}, github.ErrInvalidToken)

	// when
	resp := ExchangeGithubToken(s.T(), "bad-token")
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthSuite) Test_should_return_401_when_github_code_is_invalid() {
	// given
	githubClient.On("ExchangeCode", mock.Anything, "bad-code", "https://example.com/auth/github/callback").
		Return("", github.ErrInvalidCode)

	// when
	resp := ExchangeGithubCode(s.T(), "bad-code", "https://example.com/auth/github/callback")
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthSuite) Test_should_return_403_when_user_not_in_allowed_org() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 42, Login: "octocat"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 99, Login: "some-other-org"}}, nil)

	// when
	resp := ExchangeGithubToken(s.T(), "gh-token")
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *AuthSuite) Test_should_return_403_when_github_code_exchange_user_not_in_allowed_org() {
	// given
	githubClient.On("ExchangeCode", mock.Anything, "code-123", "https://example.com/auth/github/callback").
		Return("gh-token", nil)
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 42, Login: "octocat"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 99, Login: "some-other-org"}}, nil)

	// when
	resp := ExchangeGithubCode(s.T(), "code-123", "https://example.com/auth/github/callback")
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func subjectOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var body httpapi.TokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	parsed, err := jwt.ParseWithClaims(body.AccessToken, &jwt.RegisteredClaims{}, func(*jwt.Token) (any, error) {
		return []byte(testJWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	require.NoError(t, err)
	return parsed.Claims.(*jwt.RegisteredClaims).Subject
}
