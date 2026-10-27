package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

const (
	userAgent    = "DynamicPDB-checksum-job"
	maxRedirects = 5
)

var (
	// errNotRetryable marks a read that will not succeed on a retry: the file is gone, the
	// request is refused, the URI is unusable, or the address is not allowed. Other read errors
	// are retried.
	errNotRetryable = errors.New("not retryable")
	// errBlockedAddress is a connection to an address the job must not reach.
	errBlockedAddress = errors.New("address is not allowed")
)

// blockedPrefixes are the addresses a client-sent URI must not make the job reach: the
// cluster, the cloud metadata service and other non-public ranges.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

func isBlockedAddress(address netip.Addr) bool {
	address = address.Unmap()
	if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsInterfaceLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// NewGuardedHTTPClient is the client for client-sent URIs. Every connection, including one
// made for a redirect, is checked after DNS resolution, so a host name cannot lead the job
// to a blocked address. It ignores proxy settings, which would hide the real address.
func NewGuardedHTTPClient() *http.Client {
	return newGuardedHTTPClient(isBlockedAddress)
}

func newGuardedHTTPClient(blocked func(netip.Addr) bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_ string, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("split dial address %q: %w", address, err)
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("parse dial address %q: %w", host, err)
			}
			if blocked(ip) {
				return fmt.Errorf("%s: %w", ip, errBlockedAddress)
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   4,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects: %w", maxRedirects, errNotRetryable)
			}
			if err := checkScheme(request.URL); err != nil {
				return fmt.Errorf("redirect: %w", err)
			}
			return nil
		},
	}
}

// HTTPSource reads files that live outside our bucket, such as RCSB archive files.
type HTTPSource struct {
	client *http.Client
}

func NewHTTPSource(client *http.Client) *HTTPSource {
	return &HTTPSource{client: client}
}

// Open starts a GET of uri. The caller closes the returned body.
func (s *HTTPSource) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse uri: %w: %w", err, errNotRetryable)
	}
	if err := checkScheme(parsed); err != nil {
		return nil, fmt.Errorf("check uri: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w: %w", err, errNotRetryable)
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, errBlockedAddress) {
			return nil, fmt.Errorf("get %s: %w: %w", parsed.Host, err, errNotRetryable)
		}
		return nil, fmt.Errorf("get %s: %w", parsed.Host, err)
	}
	if response.StatusCode == http.StatusOK {
		return response.Body, nil
	}
	closeErr := response.Body.Close()
	statusErr := fmt.Errorf("http status %d", response.StatusCode)
	if !isRetryableStatus(response.StatusCode) {
		statusErr = fmt.Errorf("%w: %w", statusErr, errNotRetryable)
	}
	return nil, errors.Join(statusErr, closeErr)
}

func checkScheme(target *url.URL) error {
	switch target.Scheme {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("scheme %q is not allowed: %w", target.Scheme, errNotRetryable)
	}
}

// isRetryableStatus is true for answers that can change on a later try: server errors,
// rate limits and request timeouts.
func isRetryableStatus(status int) bool {
	return status >= 500 || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout
}
