package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/httprate"
)

// Limits for POST /v1/artifacts/{id}/extract. A request holds up to
// artifactExtractionReadLimit bytes of file plus the parser's working set (about
// 4 MiB of buffer and 5 MiB of allocations, measured in
// Test_should_stay_under_allocation_budget_when_extracting_a_four_mebibyte_head),
// so two in flight use well under 64 MiB of a 512 MiB pod. The limits exist
// because the cost per request scales with a user-supplied file, not because
// two requests are expected to be a problem.
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
