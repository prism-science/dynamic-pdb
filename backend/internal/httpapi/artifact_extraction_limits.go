package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/httprate"
)

// Limits for POST /v1/artifacts/{id}/extract. A request holds up to
// artifactExtractionReadLimit bytes of file plus the parser's working set. For a
// normal file that is about 5 MiB of allocations (Benchmark_extract_artifact_head).
// For a hostile file it is bounded too: the parser caps the rows, columns,
// items and distinct residues, instances and compounds it keeps (lib/cif/limits.go),
// and reads _struct_conn and _atom_site as streams. The worst of the hostile
// four mebibyte files in Test_should_stay_under_memory_budget_when_extracting_hostile_files
// allocates about 94 MiB in total, of which under 48 MiB is reachable at once, and
// encodes a response under 1 MiB (before the caps, one request with 2.1 million
// `a` rows in _struct_conn kept about 930 MiB live and allocated 1.4 GiB). Two in
// flight, with the 4 MiB read buffers, stay under about 210 MiB even if the
// garbage collector never ran, inside a 512 MiB pod. The limits exist because the
// cost per request still scales with a user-supplied file, not because two
// requests are expected to be a problem.
const (
	defaultExtractionMaxInFlight = 2
	// defaultExtractionUserRequests per defaultExtractionUserWindow, per user and
	// per server instance. The global limiter counts requests per client IP; this
	// one counts extraction requests per authenticated user.
	defaultExtractionUserRequests = 10
	defaultExtractionUserWindow   = time.Minute
	// extractionBusyRetryAfter is the Retry-After sent when every slot is taken.
	extractionBusyRetryAfter = 5 * time.Second
)

// extractionLimits bounds the extract endpoint: a fixed number of requests may
// read and parse a file at once, and each user may start only so many per window.
// Both limits are per server instance and in memory, like the global rate limit.
// A nil *extractionLimits imposes no limit; NewServer always installs one.
type extractionLimits struct {
	slots   chan struct{}
	perUser *httprate.RateLimiter
}

func newExtractionLimits(maxInFlight int, userRequests int, userWindow time.Duration) *extractionLimits {
	return &extractionLimits{
		slots:   make(chan struct{}, maxInFlight),
		perUser: httprate.NewRateLimiter(userRequests, userWindow),
	}
}

// userLimited reports whether the user has used up the window. When it does, the
// Retry-After header is already set on w.
func (l *extractionLimits) userLimited(w http.ResponseWriter, r *http.Request, userKey string) bool {
	if l == nil {
		return false
	}
	return l.perUser.OnLimit(w, r, userKey)
}

// tryAcquire takes a slot without waiting. The returned release is safe to call
// more than once.
func (l *extractionLimits) tryAcquire() (release func(), acquired bool) {
	if l == nil {
		return func() {}, true
	}
	select {
	case l.slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-l.slots }) }, true
	default:
		return nil, false
	}
}

func retryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(int(wait.Round(time.Second) / time.Second))
}

// WithExtractionLimits replaces the default limits of the extract endpoint.
func (s *Server) WithExtractionLimits(maxInFlight int, userRequests int, userWindow time.Duration) *Server {
	s.extractionLimits = newExtractionLimits(maxInFlight, userRequests, userWindow)
	return s
}
