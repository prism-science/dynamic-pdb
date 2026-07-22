package e2etest

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chicors "github.com/go-chi/cors"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/integrations/github"
	"dynamic-pdb/backend/internal/integrations/s3"
)

const (
	testJWTSecret = "test-secret"
	testJWTIssuer = "dynamic-pdb-test"
	testJWTTTL    = time.Hour
)

var (
	testServer   *httptest.Server
	githubClient *MockGitHubClient
)

type MockGitHubClient struct {
	mock.Mock
}

func (m *MockGitHubClient) GetUser(ctx context.Context, accessToken string) (github.User, error) {
	args := m.Called(ctx, accessToken)
	return args.Get(0).(github.User), args.Error(1)
}

func (m *MockGitHubClient) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	args := m.Called(ctx, code, redirectURI)
	return args.String(0), args.Error(1)
}

func (m *MockGitHubClient) ListOrgs(ctx context.Context, accessToken string) ([]github.Organization, error) {
	args := m.Called(ctx, accessToken)
	return args.Get(0).([]github.Organization), args.Error(1)
}

func TestMain(m *testing.M) {
	githubClient = &MockGitHubClient{}

	database, err := db.NewDB(db.Config{
		Host:             "localhost",
		Port:             35432,
		Name:             "dynamic_pdb_local",
		Username:         "postgres",
		Password:         "password",
		ConnectionParams: "sslmode=disable",
	})
	if err != nil {
		log.Fatalf("e2etest: connect db: %v", err)
	}

	authConfig := auth.Config{
		AllowedOrgs: []string{"Astera-org", "diff-use"},
		JWT: auth.JWTConfig{
			Secret: testJWTSecret,
			Issuer: testJWTIssuer,
			TTL:    testJWTTTL,
		},
	}

	jwt := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL)
	server := httpapi.NewServer(githubClient, stubUploadBucket{}, authConfig, jwt, database)

	router := chi.NewRouter()
	router.Use(corsMiddleware())
	httpapi.HandlerWithOptions(server, httpapi.ChiServerOptions{
		BaseRouter: router,
		Middlewares: []httpapi.MiddlewareFunc{
			httpapi.AuthMiddleware(jwt, database),
		},
	})
	testServer = httptest.NewServer(router)

	code := m.Run()

	testServer.Close()
	database.Close()
	os.Exit(code)
}

type stubUploadBucket struct{}

func (stubUploadBucket) PresignMultipartUpload(_ context.Context, file s3.FileUpload) (s3.MultipartUploadGrant, error) {
	key, err := s3.ObjectKey(file)
	if err != nil {
		return s3.MultipartUploadGrant{}, fmt.Errorf("build stub upload key: %w", err)
	}
	return s3.MultipartUploadGrant{
		Key:       key,
		UploadID:  "upload-id",
		ObjectURL: "s3://dynamic-pdb/" + key,
		PartSize:  64 * 1024 * 1024,
		Parts: []s3.PresignedPart{
			{PartNumber: 1, URL: "https://storage.example.test/" + key + "?part=1"},
		},
	}, nil
}

func (stubUploadBucket) CompleteMultipartUpload(_ context.Context, _, _ string, _ []s3.CompletedPart) error {
	return nil
}

func (stubUploadBucket) AbortMultipartUpload(_ context.Context, _, _ string) error {
	return nil
}

func corsMiddleware() func(http.Handler) http.Handler {
	return chicors.Handler(chicors.Options{
		AllowedOrigins: []string{
			"https://unrevealable-fleshily-brigitte.ngrok-free.dev",
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"https://*.ngrok-free.dev",
			"https://*.ngrok-free.app",
			"https://*.ngrok.io",
			"https://*.ngrok.app",
		},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodPatch,
			http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
			"Accept",
		},
		MaxAge: 300,
	})
}

type baseSuite struct {
	suite.Suite
}

func (s *baseSuite) SetupTest() {
	githubClient.ExpectedCalls = nil
	githubClient.Calls = nil
}

func (s *baseSuite) TearDownTest() {
	githubClient.AssertExpectations(s.T())
}
