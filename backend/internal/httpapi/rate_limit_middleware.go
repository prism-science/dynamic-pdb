package httpapi

import (
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/httprate"
)

// RateLimitPolicy sets how GlobalRateLimitMiddleware counts requests on one server instance.
type RateLimitPolicy struct {
	// Requests caps each client that reaches the server through the load balancer and ingress.
	Requests int
	// InternalRequests caps each in-cluster caller, such as a website pod, that calls the
	// server directly instead of through the ingress.
	InternalRequests int
	Window           time.Duration
	// TrustedProxies are the networks of the load balancer, ingress and node addresses in front
	// of the server. X-Forwarded-For entries are only believed when one of them added them.
	TrustedProxies []netip.Prefix
}

// probePaths are never rate limited, so kubelet probes keep passing while clients use up
// their buckets.
var probePaths = map[string]struct{}{
	"/livez":  {},
	"/readyz": {},
}

// GlobalRateLimitMiddleware counts requests per client IP. Clients that come through the
// ingress share policy.Requests per window; in-cluster callers get their own buckets of
// policy.InternalRequests.
func GlobalRateLimitMiddleware(policy RateLimitPolicy) func(http.Handler) http.Handler {
	limitHandler := httprate.WithLimitHandler(writeRateLimitedError(
		"too many requests from this client. Wait the number of seconds in Retry-After, then try " +
			"again; to read a long list, follow links.next instead of repeating requests"))
	ingressLimiter := httprate.NewRateLimiter(policy.Requests, policy.Window, limitHandler)
	internalLimiter := httprate.NewRateLimiter(policy.InternalRequests, policy.Window, limitHandler)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, isProbe := probePaths[r.URL.Path]; isProbe {
				next.ServeHTTP(w, r)
				return
			}
			caller := identifyCaller(r, policy.TrustedProxies)
			limiter := ingressLimiter
			if caller.internal {
				limiter = internalLimiter
			}
			if limiter.RespondOnLimit(w, r, caller.key()) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateLimitedErrorCode is the JSON:API error code of a request over a rate limit.
const rateLimitedErrorCode = "RATE_LIMITED"

// writeRateLimitedError answers a request over a rate limit with a JSON:API 429 carrying detail.
// httprate sets Retry-After and the X-RateLimit headers before it calls the handler.
func writeRateLimitedError(detail string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusTooManyRequests, rateLimitedErrorCode, detail)
	}
}

// routeRateLimiter caps the requests of one route per client, on top of the global limit.
// Clients are told apart as by GlobalRateLimitMiddleware: clients through the ingress and
// in-cluster callers have their own caps.
type routeRateLimiter struct {
	ingress        *httprate.RateLimiter
	internal       *httprate.RateLimiter
	trustedProxies []netip.Prefix
}

// newRouteRateLimiter allows requests per window to each client through the ingress and
// internalRequests to each in-cluster caller. A request over the limit gets a JSON:API 429 with
// detail and Retry-After.
func newRouteRateLimiter(
	requests, internalRequests int,
	window time.Duration,
	trustedProxies []netip.Prefix,
	detail string,
) *routeRateLimiter {
	limitHandler := httprate.WithLimitHandler(writeRateLimitedError(detail))
	return &routeRateLimiter{
		ingress:        httprate.NewRateLimiter(requests, window, limitHandler),
		internal:       httprate.NewRateLimiter(internalRequests, window, limitHandler),
		trustedProxies: trustedProxies,
	}
}

// respondOnLimit counts r and reports whether it was over the limit, in which case the 429 is
// already written.
func (l *routeRateLimiter) respondOnLimit(w http.ResponseWriter, r *http.Request) bool {
	requester := identifyCaller(r, l.trustedProxies)
	limiter := l.ingress
	if requester.internal {
		limiter = l.internal
	}
	return limiter.RespondOnLimit(w, r, requester.key())
}

// caller is who a request is counted against.
type caller struct {
	// address is invalid when the request carries no readable address.
	address netip.Addr
	// internal marks an in-cluster caller that reached the server without the ingress.
	internal bool
}

func (c caller) key() string {
	if !c.address.IsValid() {
		return ""
	}
	return httprate.CanonicalizeIP(c.address.String())
}

// identifyCaller finds who sent r.
//
// A peer outside trustedProxies is the client, and its X-Forwarded-For is ignored. A trusted
// peer that sends no X-Forwarded-For is an in-cluster caller, because the ingress always adds
// the header. Otherwise X-Forwarded-For is read right to left, skipping trusted proxies, and
// the first other address is the client. When every address is a trusted proxy, the leftmost
// one is used.
func identifyCaller(r *http.Request, trustedProxies []netip.Prefix) caller {
	peer := peerAddress(r.RemoteAddr)
	if !peer.IsValid() || !isTrustedProxy(peer, trustedProxies) {
		return caller{address: peer}
	}
	forwardedFor := r.Header.Values("X-Forwarded-For")
	if len(forwardedFor) == 0 {
		return caller{address: peer, internal: true}
	}

	client := peer
	entries := forwardedEntries(forwardedFor)
	for i := len(entries) - 1; i >= 0; i-- {
		address, err := netip.ParseAddr(entries[i])
		if err != nil {
			// Nothing left of an unreadable entry can be trusted.
			break
		}
		client = address.Unmap().WithZone("")
		if !isTrustedProxy(client, trustedProxies) {
			break
		}
	}
	return caller{address: client}
}

func peerAddress(remoteAddr string) netip.Addr {
	if addressPort, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return addressPort.Addr().Unmap().WithZone("")
	}
	if address, err := netip.ParseAddr(remoteAddr); err == nil {
		return address.Unmap().WithZone("")
	}
	return netip.Addr{}
}

// forwardedEntries merges every X-Forwarded-For header into one list, oldest hop first.
func forwardedEntries(headerValues []string) []string {
	var entries []string
	for _, value := range headerValues {
		for _, entry := range strings.Split(value, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func isTrustedProxy(address netip.Addr, trustedProxies []netip.Prefix) bool {
	for _, prefix := range trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
