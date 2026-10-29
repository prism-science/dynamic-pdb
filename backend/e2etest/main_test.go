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
	"dynamic-pdb/backend/internal/services/analytics"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/backend/internal/types"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

// testPublicBaseURL stands in for the ingress URL, whose /api prefix this server never sees.
const testPublicBaseURL = "https://dynamicpdb.example/api"

const (
	testJWTSecret = "test-secret"
	testJWTIssuer = "dynamic-pdb-test"
	testJWTTTL    = time.Hour
)

var (
	testServer *httptest.Server
	// logOnlyTestServer shares the database with testServer but only logs schema violations.
	logOnlyTestServer *httptest.Server
	// limitedExtractionServer allows one extraction at a time and three per user per minute.
	limitedExtractionServer *httptest.Server
	// timeoutExtractionServer allows one extraction at a time and gives the parser no time at all.
	timeoutExtractionServer *httptest.Server
	// selfApprovalServer shares the database with testServer and lets reviewers approve
	// revisions they created, as on dev.
	selfApprovalServer *httptest.Server
	// entryDeletionServer shares the database with testServer and has entry deletion turned on.
	entryDeletionServer *httptest.Server
	testDatabase        *db.DB
	githubClient        *MockGitHubClient
	s3Stub              *e2es3.StubServer
	adminToken          string
)

// testRouterOptions configures one of the test servers that TestMain starts.
type testRouterOptions struct {
	validationMode validation.Mode
	// limitExtraction allows one extraction at a time and three per user per minute.
	limitExtraction bool
	// extractionTimeout, when set, replaces the time one extraction may spend parsing.
	extractionTimeout time.Duration
	// entryDeletion turns DELETE /v1/entries/{entry_id} on.
	entryDeletion bool
	// selfApproval lets reviewers approve revisions they created.
	selfApproval bool
}

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

	var err error
	testDatabase, err = db.NewDB(db.Config{
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
	admin, err := testDatabase.Users.Create(context.Background(), models.User{
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
	adminRoles, err := testDatabase.Roles.List(context.Background(), db.RoleFilters{
		Keys: []models.RoleKey{models.RoleKeyAdmin, models.RoleKeyReviewer},
	})
	if err != nil {
		log.Fatalf("e2etest: list admin roles: %v", err)
	}
	if len(adminRoles) != 2 {
		log.Fatalf("e2etest: expected admin and reviewer roles, got %d", len(adminRoles))
	}
	for _, role := range adminRoles {
		if err := testDatabase.Roles.AssignToUser(context.Background(), admin.ID, role.ID, nil); err != nil {
			log.Fatalf("e2etest: assign %s role: %v", role.Key, err)
		}
	}

	authConfig := auth.Config{
		AllowedOrgs: []string{"Astera-org", "prism-science"},
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
		Endpoint:          s3Stub.URL(),
		Region:            "us-east-1",
		Bucket:            "dynamic-pdb",
		AccessKeyID:       "AKIAEXAMPLE",
		SecretAccessKey:   "secret",
		UploadMaxFileSize: 1 << 30,
		UploadURLTTL:      15 * time.Minute,
	}
	fileUploadBucket, err := storage.NewBucket(context.Background(), storageConfig)
	if err != nil {
		log.Fatalf("e2etest: create s3 bucket: %v", err)
	}
	fileCDN, err := cdn.NewService(fileUploadBucket, cdn.Config{S3: storageConfig})
	if err != nil {
		log.Fatalf("e2etest: create CDN service: %v", err)
	}
	rcsbStub := httptest.NewServer(http.HandlerFunc(serveArchiveFixture))
	archiveLookup := archive.NewLookup(
		rcsb.NewClient(rcsb.WithBaseURLs(rcsbStub.URL, rcsbStub.URL, rcsbStub.URL, rcsbStub.URL)),
		sifts.NewClient(sifts.WithBaseURL(rcsbStub.URL)),
	)
	newTestRouter := func(options testRouterOptions) http.Handler {
		server := httpapi.NewServer(
			githubClient, fileCDN, authConfig, jwt, testDatabase, analytics.New(analytics.Config{}), testPublicBaseURL,
			options.validationMode,
		).WithArchiveLookup(archiveLookup).WithEntryDeletion(options.entryDeletion).WithSelfApproval(options.selfApproval)
		if options.limitExtraction {
			server.WithExtractionLimits(1, 3, time.Minute)
		}
		if options.extractionTimeout > 0 {
			server.WithExtractionLimits(1, 100, time.Minute).WithExtractionTimeout(options.extractionTimeout)
		}
		router := chi.NewRouter()
		router.Use(corsMiddleware())
		router.Use(httpapi.MediaTypeMiddleware())
		httpapi.HandlerWithOptions(server, httpapi.ChiServerOptions{
			BaseRouter:       router,
			ErrorHandlerFunc: httpapi.RouteErrorHandler,
			Middlewares: []httpapi.MiddlewareFunc{
				httpapi.AuthMiddleware(jwt, testDatabase),
			},
		})
		return router
	}
	testServer = httptest.NewServer(newTestRouter(testRouterOptions{validationMode: validation.ModeEnforce}))
	logOnlyTestServer = httptest.NewServer(newTestRouter(testRouterOptions{validationMode: validation.ModeLogOnly}))
	limitedExtractionServer = httptest.NewServer(newTestRouter(testRouterOptions{
		validationMode:  validation.ModeEnforce,
		limitExtraction: true,
	}))
	timeoutExtractionServer = httptest.NewServer(newTestRouter(testRouterOptions{
		validationMode:    validation.ModeEnforce,
		extractionTimeout: time.Nanosecond,
	}))
	entryDeletionServer = httptest.NewServer(newTestRouter(testRouterOptions{
		validationMode: validation.ModeEnforce,
		entryDeletion:  true,
	}))
	selfApprovalServer = httptest.NewServer(newTestRouter(testRouterOptions{
		validationMode: validation.ModeEnforce,
		selfApproval:   true,
	}))

	code := m.Run()

	testServer.Close()
	logOnlyTestServer.Close()
	limitedExtractionServer.Close()
	timeoutExtractionServer.Close()
	entryDeletionServer.Close()
	selfApprovalServer.Close()
	s3Stub.Close()
	rcsbStub.Close()
	if err := testDatabase.Close(); err != nil {
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
			http.MethodHead,
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
