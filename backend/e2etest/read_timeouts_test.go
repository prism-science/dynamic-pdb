package e2etest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	storage "dynamic-pdb/backend/internal/integrations/s3"
	"dynamic-pdb/backend/internal/services/analytics"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

// ReadTimeoutsSuite covers ticket 4's timeout rules through the request middleware cmd/server
// uses: a read that runs past its deadline answers 504 and is logged, a client that hangs up
// stops its database query, and stored records stay readable while RCSB is down.
type ReadTimeoutsSuite struct {
	baseSuite
}

func TestReadTimeouts(t *testing.T) {
	suite.Run(t, new(ReadTimeoutsSuite))
}

func (s *ReadTimeoutsSuite) Test_should_answer_504_and_log_when_read_runs_past_its_deadline() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "read-timeout-owner", 8901)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry read past its deadline " + uuid.NewString()},
	})
	server := startLifecycleTestServer(s.T(), 300*time.Millisecond)
	releaseLock := lockSimilarityTableForTest(s.T())
	defer releaseLock()
	path := fmt.Sprintf("/v1/entries/%s/similar-entries", entry.EntryId)
	started := time.Now()

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), server.URL, http.MethodGet, path, nil, "")

	// then
	s.Less(time.Since(started), 3*time.Second)
	s.Require().Equal(http.StatusGatewayTimeout, response.StatusCode)
	assertErrorResponse(s.T(), response, "TIMEOUT", "time limit")
	// The lock is still held, so no waiting query means the timed-out query was cancelled.
	server.awaitLockWaitingQueries(s.T(), 0, 2*time.Second)
	logLine := server.logs.awaitAccessLogLine(s.T(), path)
	s.Equal("ERROR", logLine["level"])
	s.InDelta(http.StatusGatewayTimeout, logLine["status"], 0)
	s.Equal("/v1/entries/{entry_id}/similar-entries", logLine["route"])
	s.Equal("context deadline exceeded", logLine["context_error"])
	s.GreaterOrEqual(logLine["duration_ms"], float64(300))
}

func (s *ReadTimeoutsSuite) Test_should_stop_database_query_when_client_hangs_up_during_read() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "read-cancel-owner", 8902)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry read by a client that hangs up " + uuid.NewString()},
	})
	// A minute-long deadline, so only the client hanging up can stop this read.
	server := startLifecycleTestServer(s.T(), time.Minute)
	releaseLock := lockSimilarityTableForTest(s.T())
	defer releaseLock()
	path := fmt.Sprintf("/v1/entries/%s/similar-entries", entry.EntryId)
	clientContext, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	request, err := http.NewRequestWithContext(clientContext, http.MethodGet, server.URL+path, nil)
	s.Require().NoError(err)
	request.Header.Set("Accept", httpapi.JSONAPIMediaType)
	clientErrors := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			err = errors.Join(fmt.Errorf("unexpected response %d", response.StatusCode), response.Body.Close())
		}
		clientErrors <- err
	}()
	// The read has reached the database and waits for the lock.
	server.awaitLockWaitingQueries(s.T(), 1, 3*time.Second)

	// when
	hangUp()

	// then
	s.Require().ErrorIs(<-clientErrors, context.Canceled)
	// The lock is still held, so no waiting query means the abandoned query was cancelled.
	server.awaitLockWaitingQueries(s.T(), 0, 2*time.Second)
	logLine := server.logs.awaitAccessLogLine(s.T(), path)
	s.Equal("/v1/entries/{entry_id}/similar-entries", logLine["route"])
	s.Equal("context canceled", logLine["context_error"])
	s.InDelta(http.StatusInternalServerError, logLine["status"], 0)
}

func (s *ReadTimeoutsSuite) Test_should_serve_stored_entry_when_rcsb_is_unreachable() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "rcsb-down-owner", 8903)
	pdbID := unusedPDBIDForTest(s.T())
	title := "entry read while RCSB is down " + uuid.NewString()
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": title, "external_refs": map[string]string{"pdb": pdbID}},
	})
	model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title":    "model read while RCSB is down",
			"metadata": map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{
				artifactRequest(uuid.New(), "FASTA", "L2", "fasta", "s3://model/rcsb-down.fasta", map[string]any{
					"records": []map[string]any{{"header": "rcsb down", "sequence": "MACDEFGHIK"}},
				}),
			},
		},
	})
	// The archive lookup retries RCSB until the request deadline, so a short deadline keeps
	// this test fast; the stored reads finish long before it.
	server := startLifecycleTestServer(s.T(), 2*time.Second)
	get := func(path string) *http.Response {
		// The archive lookup needs a signed-in user; the other reads are public.
		return jsonRequestToServerWithToken(s.T(), server.URL, http.MethodGet, path, nil, ownerToken)
	}

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	entryDocument := decodeJSONResponse[httpapi.EntryDocument](s.T(), get("/v1/entries/"+entry.EntryId), http.StatusOK)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	listDocument := decodeJSONResponse[httpapi.EntryCollectionDocument](s.T(),
		get("/v1/entries?pdb_id="+url.QueryEscape(pdbID)), http.StatusOK)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	modelsDocument := decodeJSONResponse[httpapi.ModelCollectionDocument](s.T(),
		get(fmt.Sprintf("/v1/entries/%s/models", entry.EntryId)), http.StatusOK)
	//nolint:bodyclose // assertErrorResponse closes the response body.
	archiveResponse := get("/v1/archive/pdb/" + pdbID)

	// then
	s.Equal(title, stringValue(entryDocument.Data.Attributes.Title))
	s.Require().Len(listDocument.Data, 1)
	s.Equal(entry.EntryId, listDocument.Data[0].Id)
	s.Require().Len(modelsDocument.Data, 1)
	s.Equal(model.ModelId, modelsDocument.Data[0].Attributes.Id)
	s.Equal(http.StatusBadGateway, archiveResponse.StatusCode)
	assertErrorResponse(s.T(), archiveResponse, "BAD_GATEWAY", "failed to read the entry from RCSB")
}

// lifecycleTestServer is an API server behind httpapi.RequestLifecycleMiddlewares, wrapped as
// cmd/server wraps it. Its RCSB and SIFTS clients point at a closed port, as if both were
// down; the timeout tests never reach them. It has its own database pool whose
// application_name lets a test find this server's queries in pg_stat_activity.
type lifecycleTestServer struct {
	*httptest.Server
	applicationName string
	logs            *accessLogBuffer
	// rawDatabase is a connection outside the server's pool, for watching its queries.
	rawDatabase *sqlx.DB
}

func startLifecycleTestServer(t *testing.T, readTimeout time.Duration) lifecycleTestServer {
	t.Helper()

	applicationName := "e2e-read-timeouts-" + uuid.NewString()[:8]
	database, err := db.NewDB(db.Config{
		Host:             "localhost",
		Port:             35432,
		Name:             "dynamic_pdb_local",
		Username:         "postgres",
		Password:         "password",
		ConnectionParams: testSchemaConnectionParams + " application_name=" + applicationName,
		// The statement timeout every deployed config uses; the read deadlines here are shorter.
		StatementTimeout: 10 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	closedPort := httptest.NewServer(http.NotFoundHandler())
	closedPort.Close()
	unreachableArchive := archive.NewLookup(
		rcsb.NewClient(rcsb.WithBaseURLs(closedPort.URL, closedPort.URL, closedPort.URL, closedPort.URL)),
		sifts.NewClient(sifts.WithBaseURL(closedPort.URL)),
	)

	// Tokens issued by testServer stay valid here: the JWT settings and the users are shared.
	jwt := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL)
	authConfig := auth.Config{JWT: auth.JWTConfig{Secret: testJWTSecret, Issuer: testJWTIssuer, TTL: testJWTTTL}}
	server := httpapi.NewServer(
		githubClient, newStubFileCDNForTest(t), authConfig, jwt, database, analytics.New(analytics.Config{}),
		testPublicBaseURL, enforceAllValidation,
	).WithArchiveLookup(unreachableArchive)

	logs := &accessLogBuffer{}
	router := chi.NewRouter()
	router.Use(httpapi.RequestLifecycleMiddlewares(slog.New(slog.NewJSONHandler(logs, nil)), readTimeout)...)
	router.Use(httpapi.MediaTypeMiddleware())
	httpapi.HandlerWithOptions(server, httpapi.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: httpapi.RouteErrorHandler,
		Middlewares:      []httpapi.MiddlewareFunc{httpapi.AuthMiddleware(jwt, database)},
	})
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

	rawDatabase, err := sqlx.Connect("postgres", rawTestDatabaseDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rawDatabase.Close()) })

	return lifecycleTestServer{Server: httpServer, applicationName: applicationName, logs: logs, rawDatabase: rawDatabase}
}

// awaitLockWaitingQueries waits until exactly want of this server's statements are waiting
// for a lock.
func (s lifecycleTestServer) awaitLockWaitingQueries(t *testing.T, want int, within time.Duration) {
	t.Helper()

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var waiting int
		err := s.rawDatabase.Get(&waiting, `select count(*) from pg_stat_activity
			where application_name = $1 and state = 'active' and wait_event_type = 'Lock'`, s.applicationName)
		require.NoError(collect, err)
		assert.Equal(collect, want, waiting, "statements of this server waiting for a lock")
	}, within, 20*time.Millisecond)
}

// newStubFileCDNForTest builds the file CDN over the same S3 stub TestMain uses.
func newStubFileCDNForTest(t *testing.T) cdn.Service {
	t.Helper()

	storageConfig := storage.BucketConfig{
		Endpoint:          s3Stub.URL(),
		Region:            "us-east-1",
		Bucket:            "dynamic-pdb",
		AccessKeyID:       "AKIAEXAMPLE",
		SecretAccessKey:   "secret",
		UploadMaxFileSize: 1 << 30,
		UploadURLTTL:      15 * time.Minute,
	}
	bucket, err := storage.NewBucket(t.Context(), storageConfig)
	require.NoError(t, err)
	fileCDN, err := cdn.NewService(bucket, cdn.Config{S3: storageConfig})
	require.NoError(t, err)
	return fileCDN
}

// lockSimilarityTableForTest holds an ACCESS EXCLUSIVE lock on protein_sequence_similarities
// until the returned function runs, so every read of that table waits. Only the
// similar-entries endpoint reads it, and tests in other packages that share this database wait
// at most as long as the lock is held. idle_in_transaction_session_timeout ends the lock after
// 5 s even if the test dies before releasing it.
func lockSimilarityTableForTest(t *testing.T) (release func()) {
	t.Helper()

	rawDatabase, err := sqlx.Connect("postgres", rawTestDatabaseDSN)
	require.NoError(t, err)
	transaction, err := rawDatabase.Beginx()
	require.NoError(t, err)
	_, err = transaction.Exec(`set local idle_in_transaction_session_timeout = '5s'`)
	require.NoError(t, err)
	_, err = transaction.Exec(`lock table protein_sequence_similarities in access exclusive mode`)
	require.NoError(t, err)

	var once sync.Once
	release = func() {
		once.Do(func() {
			require.NoError(t, transaction.Rollback())
			require.NoError(t, rawDatabase.Close())
		})
	}
	t.Cleanup(release)
	return release
}

// accessLogBuffer collects a test server's JSON log lines; server goroutines write while the
// test reads.
type accessLogBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *accessLogBuffer) Write(line []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(line)
}

// awaitAccessLogLine waits for the access log line of the request to path and returns it.
// The line is written only after the handler returns, which can be after the client gave up.
func (b *accessLogBuffer) awaitAccessLogLine(t *testing.T, path string) map[string]any {
	t.Helper()

	var found map[string]any
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		line, err := b.accessLogLine(path)
		require.NoError(collect, err)
		require.NotNil(collect, line, "access log line for %s", path)
		found = line
	}, 2*time.Second, 20*time.Millisecond)
	return found
}

// accessLogLine returns the access log line of the request to path, or nil when there is none yet.
func (b *accessLogBuffer) accessLogLine(path string) (map[string]any, error) {
	b.mutex.Lock()
	contents := b.buffer.String()
	b.mutex.Unlock()
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return nil, fmt.Errorf("decode log line %q: %w", scanner.Text(), err)
		}
		if line["msg"] == "http request" && line["path"] == path {
			return line, nil
		}
	}
	return nil, scanner.Err()
}
