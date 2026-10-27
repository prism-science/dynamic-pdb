package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The addresses below follow the production path: AWS load balancer (VPC 10.7.0.0/16) →
// kube-proxy, which rewrites the source to a node address (pod network 10.42.0.0/16) →
// ingress pod → server.
const (
	ingressPodPeer   = "10.42.3.195:41000"
	nodeAddress      = "10.42.3.0"
	kubeletProbePeer = "10.42.1.1:52000"
	websitePodPeer   = "10.42.3.35:39000"
)

var clusterProxies = []netip.Prefix{
	netip.MustParsePrefix("10.42.0.0/16"),
	netip.MustParsePrefix("10.7.0.0/16"),
}

func newRateLimitedHandler(requests, internalRequests int) http.Handler {
	policy := RateLimitPolicy{
		Requests:         requests,
		InternalRequests: internalRequests,
		Window:           time.Minute,
		TrustedProxies:   clusterProxies,
	}
	return GlobalRateLimitMiddleware(policy)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func newRequest(path, peer string, forwardedFor ...string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = peer
	for _, value := range forwardedFor {
		request.Header.Add("X-Forwarded-For", value)
	}
	return request
}

func send(handler http.Handler, request *http.Request) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func Test_should_answer_probes_when_caller_used_up_its_bucket(t *testing.T) {
	for _, path := range []string{"/livez", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			// given
			handler := newRateLimitedHandler(2, 2)
			for range 2 {
				require.Equal(t, http.StatusOK, send(handler, newRequest("/v1/entries", kubeletProbePeer)))
			}
			require.Equal(t, http.StatusTooManyRequests, send(handler, newRequest("/v1/entries", kubeletProbePeer)))

			// when
			status := send(handler, newRequest(path, kubeletProbePeer))

			// then
			assert.Equal(t, http.StatusOK, status)
		})
	}
}

func Test_should_count_clients_behind_ingress_separately_when_one_used_up_its_bucket(t *testing.T) {
	// given
	handler := newRateLimitedHandler(2, 100)
	for range 2 {
		require.Equal(t, http.StatusOK, send(handler, newRequest("/v1/entries", ingressPodPeer, "198.51.100.1, "+nodeAddress)))
	}
	require.Equal(t, http.StatusTooManyRequests, send(handler, newRequest("/v1/entries", ingressPodPeer, "198.51.100.1, "+nodeAddress)))

	// when
	status := send(handler, newRequest("/v1/entries", ingressPodPeer, "198.51.100.2, "+nodeAddress))

	// then
	assert.Equal(t, http.StatusOK, status)
}

func Test_should_limit_in_cluster_caller_with_internal_limit_when_it_calls_directly(t *testing.T) {
	// given
	handler := newRateLimitedHandler(1, 3)
	for range 3 {
		require.Equal(t, http.StatusOK, send(handler, newRequest("/v1/entries", websitePodPeer)))
	}

	// when
	status := send(handler, newRequest("/v1/entries", websitePodPeer))

	// then
	assert.Equal(t, http.StatusTooManyRequests, status)
}

func Test_should_not_count_in_cluster_caller_against_ingress_clients_when_it_calls_directly(t *testing.T) {
	// given
	handler := newRateLimitedHandler(1, 100)
	for range 5 {
		require.Equal(t, http.StatusOK, send(handler, newRequest("/v1/entries", websitePodPeer)))
	}

	// when
	status := send(handler, newRequest("/v1/entries", ingressPodPeer, "198.51.100.1, "+nodeAddress))

	// then
	assert.Equal(t, http.StatusOK, status)
}

func Test_should_reject_request_when_client_without_proxy_exceeded_limit(t *testing.T) {
	// given
	handler := newRateLimitedHandler(3, 100)
	for range 3 {
		require.Equal(t, http.StatusOK, send(handler, newRequest("/v1/entries", "192.0.2.1:1234")))
	}

	// when
	status := send(handler, newRequest("/v1/entries", "192.0.2.1:1234"))

	// then
	assert.Equal(t, http.StatusTooManyRequests, status)
}

func Test_should_use_peer_and_ignore_forwarded_for_when_peer_is_not_trusted_proxy(t *testing.T) {
	// given
	request := newRequest("/", "192.0.2.1:1234", "198.51.100.1")

	// when
	identified := identifyCaller(request, clusterProxies)

	// then
	assert.Equal(t, caller{address: netip.MustParseAddr("192.0.2.1")}, identified)
}

func Test_should_use_address_added_by_load_balancer_when_request_comes_through_ingress(t *testing.T) {
	// given
	request := newRequest("/", ingressPodPeer, "198.51.100.1, "+nodeAddress)

	// when
	identified := identifyCaller(request, clusterProxies)

	// then
	assert.Equal(t, caller{address: netip.MustParseAddr("198.51.100.1")}, identified)
}

func Test_should_ignore_addresses_client_sent_itself_when_request_comes_through_ingress(t *testing.T) {
	// given
	request := newRequest("/", ingressPodPeer, "203.0.113.9, 10.42.0.9", "198.51.100.1, 10.7.1.20, "+nodeAddress)

	// when
	identified := identifyCaller(request, clusterProxies)

	// then
	assert.Equal(t, caller{address: netip.MustParseAddr("198.51.100.1")}, identified)
}

func Test_should_use_leftmost_proxy_when_every_forwarded_address_is_trusted(t *testing.T) {
	// given
	request := newRequest("/", ingressPodPeer, nodeAddress)

	// when
	identified := identifyCaller(request, clusterProxies)

	// then
	assert.Equal(t, caller{address: netip.MustParseAddr(nodeAddress)}, identified)
}

func Test_should_stop_at_last_readable_proxy_when_forwarded_entry_is_unreadable(t *testing.T) {
	// given
	request := newRequest("/", ingressPodPeer, "198.51.100.1, unknown, "+nodeAddress)

	// when
	identified := identifyCaller(request, clusterProxies)

	// then
	assert.Equal(t, caller{address: netip.MustParseAddr(nodeAddress)}, identified)
}

func Test_should_mark_caller_internal_when_trusted_peer_sends_no_forwarded_for(t *testing.T) {
	// given
	request := newRequest("/", websitePodPeer)

	// when
	identified := identifyCaller(request, clusterProxies)

	// then
	assert.Equal(t, caller{address: netip.MustParseAddr("10.42.3.35"), internal: true}, identified)
}

func Test_should_treat_every_peer_as_client_when_no_proxy_is_trusted(t *testing.T) {
	// given
	request := newRequest("/", websitePodPeer, "198.51.100.1")

	// when
	identified := identifyCaller(request, nil)

	// then
	assert.Equal(t, caller{address: netip.MustParseAddr("10.42.3.35")}, identified)
}
