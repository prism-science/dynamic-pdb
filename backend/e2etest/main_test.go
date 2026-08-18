package e2etest

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chicors "github.com/go-chi/cors"
	"github.com/google/uuid"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	e2es3 "dynamic-pdb/backend/e2etest/s3"
	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/integrations/github"
	storage "dynamic-pdb/backend/internal/integrations/s3"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/backend/internal/types"
)

const (
	testJWTSecret = "test-secret"
	testJWTIssuer = "dynamic-pdb-test"
	testJWTTTL    = time.Hour
)

var (
	testServer   *httptest.Server
	githubClient *MockGitHubClient
	s3Stub       *e2es3.StubServer
	adminToken   string
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
	s3Stub = e2es3.NewStubServer()

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

	now := time.Now().UTC()
	admin, err := database.Users.Create(context.Background(), models.User{
		ID: uuid.MustParse("8ca59596-c4b4-4f3f-94be-6dd73f76f050"),
		ExternalRef: types.ExternalRef{
			Source: "e2etest",
			Value:  "admin",
		},
		DisplayName: "E2E Admin",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		log.Fatalf("e2etest: create admin: %v", err)
	}

	authConfig := auth.Config{
		AllowedOrgs:  []string{"Astera-org", "diff-use"},
		AdminUserIDs: []string{admin.ID.String()},
		JWT: auth.JWTConfig{
			Secret: testJWTSecret,
			Issuer: testJWTIssuer,
			TTL:    testJWTTTL,
		},
	}

	jwt := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL)
	adminToken, _, err = jwt.Issue(admin.ID)
	if err != nil {
		log.Fatalf("e2etest: issue admin token: %v", err)
	}
	storageConfig := storage.BucketConfig{
		Endpoint:        s3Stub.URL(),
		Region:          "us-east-1",
		Bucket:          "dynamic-pdb",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
	}
	fileUploadBucket, err := storage.NewBucket(context.Background(), storageConfig)
	if err != nil {
		log.Fatalf("e2etest: create s3 bucket: %v", err)
	}
	fileCDN, err := cdn.NewService(fileUploadBucket, cdn.Config{S3: storageConfig})
	if err != nil {
		log.Fatalf("e2etest: create CDN service: %v", err)
	}
	server := httpapi.NewServer(githubClient, fileCDN, authConfig, jwt, database)

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
	s3Stub.Close()
	if err := database.Close(); err != nil {
		log.Printf("e2etest: close database: %v", err)
	}
	os.Exit(code)
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
	s3Stub.Reset()
}

func (s *baseSuite) TearDownTest() {
	githubClient.AssertExpectations(s.T())
}
