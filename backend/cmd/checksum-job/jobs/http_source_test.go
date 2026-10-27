package jobs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_block_non_public_addresses_when_address_checked(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.102.144.42",
		"0.0.0.0", "224.0.0.1", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::a9fe:a9fe",
	}
	allowed := []string{"132.249.213.140", "8.8.8.8", "2606:4700::1111"}
	for _, address := range blocked {
		t.Run(address, func(t *testing.T) {
			// given
			ip := netip.MustParseAddr(address)

			// when
			isBlocked := isBlockedAddress(ip)

			// then
			assert.True(t, isBlocked)
		})
	}
	for _, address := range allowed {
		t.Run(address, func(t *testing.T) {
			// given
			ip := netip.MustParseAddr(address)

			// when
			isBlocked := isBlockedAddress(ip)

			// then
			assert.False(t, isBlocked)
		})
	}
}

func Test_should_refuse_loopback_server_when_guarded_client_connects(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("secret"))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	source := NewHTTPSource(NewGuardedHTTPClient())

	// when
	_, err := source.Open(context.Background(), server.URL+"/file.cif")

	// then
	require.ErrorIs(t, err, errBlockedAddress)
	require.ErrorIs(t, err, errNotRetryable)
}

func Test_should_refuse_metadata_address_when_server_redirects_there(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	onlyTestServerAllowed := func(address netip.Addr) bool {
		return address != netip.MustParseAddr("127.0.0.1")
	}
	source := NewHTTPSource(newGuardedHTTPClient(onlyTestServerAllowed))

	// when
	_, err := source.Open(context.Background(), server.URL+"/file.cif")

	// then
	require.ErrorIs(t, err, errBlockedAddress)
	require.ErrorIs(t, err, errNotRetryable)
}

func Test_should_refuse_uri_when_scheme_is_not_http(t *testing.T) {
	for _, uri := range []string{"file:///etc/passwd", "ftp://files.rcsb.org/1abc.cif", "s3://bucket/key"} {
		t.Run(uri, func(t *testing.T) {
			// given
			source := NewHTTPSource(NewGuardedHTTPClient())

			// when
			_, err := source.Open(context.Background(), uri)

			// then
			require.ErrorIs(t, err, errNotRetryable)
		})
	}
}

func Test_should_stream_body_and_send_user_agent_when_server_answers_ok(t *testing.T) {
	// given
	var receivedUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		_, err := w.Write([]byte("data_1abc\n"))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	source := NewHTTPSource(server.Client())

	// when
	body, err := source.Open(context.Background(), server.URL+"/1abc.cif")

	// then
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	assert.Equal(t, "data_1abc\n", string(data))
	assert.Equal(t, "DynamicPDB-checksum-job", receivedUserAgent)
}

func Test_should_mark_file_gone_only_when_status_will_not_change(t *testing.T) {
	cases := map[int]bool{
		http.StatusNotFound:            true,
		http.StatusGone:                true,
		http.StatusForbidden:           true,
		http.StatusInternalServerError: false,
		http.StatusServiceUnavailable:  false,
		http.StatusTooManyRequests:     false,
		http.StatusRequestTimeout:      false,
	}
	for status, gone := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			// given
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)
			source := NewHTTPSource(server.Client())

			// when
			_, err := source.Open(context.Background(), server.URL+"/1abc.cif")

			// then
			require.Error(t, err)
			assert.Equal(t, gone, errors.Is(err, errNotRetryable))
		})
	}
}
