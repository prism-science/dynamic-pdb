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
	s.NotNil(body.Permissions)
	s.Empty(body.Permissions)

	parsed, err := jwt.ParseWithClaims(body.AccessToken, &jwt.RegisteredClaims{}, func(*jwt.Token) (any, error) {
		return []byte(testJWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	s.Require().NoError(err)

	claims := parsed.Claims.(*jwt.RegisteredClaims)
	s.Equal(testJWTIssuer, claims.Issuer)
	_, err = uuid.Parse(claims.Subject)
	s.NoError(err, "subject should be a valid UUID")
}

func (s *AuthSuite) Test_should_return_login_as_name_when_github_name_is_empty() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 4201, Login: "octocat"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	// when
	resp := ExchangeGithubToken(s.T(), "gh-token")
	defer resp.Body.Close()

	// then
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body httpapi.TokenResponse
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal("octocat", body.Name)
	s.Equal("", body.Email)
	s.Equal("octocat", body.Login)
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
	s.NotNil(body.Permissions)
	s.Empty(body.Permissions)
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

func (s *AuthSuite) Test_should_map_github_code_exchange_errors_to_http_responses() {
	tests := []struct {
		name       string
		code       string
		err        error
		statusCode int
		errorCode  string
	}{
		{
			name:       "access denied",
			code:       "denied-code",
			err:        github.ErrAccessDenied,
			statusCode: http.StatusUnauthorized,
			errorCode:  "GITHUB_ACCESS_DENIED",
		},
		{
			name:       "redirect uri mismatch",
			code:       "redirect-mismatch-code",
			err:        github.ErrRedirectURIMismatch,
			statusCode: http.StatusBadRequest,
			errorCode:  "INVALID_REDIRECT_URI",
		},
		{
			name:       "oauth not configured",
			code:       "oauth-missing-code",
			err:        github.ErrOAuthNotConfigured,
			statusCode: http.StatusInternalServerError,
			errorCode:  "INTERNAL_ERROR",
		},
		{
			name:       "incorrect credentials",
			code:       "bad-client-code",
			err:        github.ErrIncorrectClientCredentials,
			statusCode: http.StatusInternalServerError,
			errorCode:  "INTERNAL_ERROR",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// given
			githubClient.On("ExchangeCode", mock.Anything, tt.code, "https://example.com/auth/github/callback").
				Return("", tt.err)

			// when
			resp := ExchangeGithubCode(s.T(), tt.code, "https://example.com/auth/github/callback")
			defer resp.Body.Close()

			// then
			s.Equal(tt.statusCode, resp.StatusCode)

			var body httpapi.Error
			s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
			s.Equal(tt.errorCode, body.Code)
		})
	}
}

func (s *AuthSuite) Test_should_return_500_when_github_user_or_org_lookup_fails() {
	tests := []struct {
		name        string
		accessToken string
		mockCalls   func()
	}{
		{
			name:        "get user fails",
			accessToken: "get-user-fails",
			mockCalls: func() {
				githubClient.On("GetUser", mock.Anything, "get-user-fails").
					Return(github.User{}, assertableError("get user failed"))
			},
		},
		{
			name:        "list orgs fails",
			accessToken: "list-orgs-fails",
			mockCalls: func() {
				githubClient.On("GetUser", mock.Anything, "list-orgs-fails").
					Return(github.User{ID: 42, Login: "octocat"}, nil)
				githubClient.On("ListOrgs", mock.Anything, "list-orgs-fails").
					Return([]github.Organization{}, assertableError("list orgs failed"))
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// given
			tt.mockCalls()

			// when
			resp := ExchangeGithubToken(s.T(), tt.accessToken)
			defer resp.Body.Close()

			// then
			s.Equal(http.StatusInternalServerError, resp.StatusCode)

			var body httpapi.Error
			s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
			s.Equal("INTERNAL_ERROR", body.Code)
		})
	}
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

type assertableError string

func (e assertableError) Error() string {
	return string(e)
}
