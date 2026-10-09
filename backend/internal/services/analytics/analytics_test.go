package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_send_download_event_when_model_file_requested(t *testing.T) {
	// given
	received := make(chan sendRequest, 1)
	userAgents := make(chan string, 1)
	umamiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event sendRequest
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		userAgents <- r.UserAgent()
		received <- event
	}))
	defer umamiServer.Close()
	tracker := New(Config{Endpoint: umamiServer.URL, WebsiteID: "site-id", Hostname: "dynamicpdb.com"})

	// when
	tracker.TrackDownload(Download{
		Path:      "/v1/files/dpdb_1/m1/dpdb_1.cif",
		EntryID:   "dpdb_1",
		ModelID:   "m1",
		Filename:  "dpdb_1.cif",
		UserAgent: "curl/8.7.1",
	})

	// then
	select {
	case event := <-received:
		assert.Equal(t, "event", event.Type)
		assert.Equal(t, "site-id", event.Payload.Website)
		assert.Equal(t, "dynamicpdb.com", event.Payload.Hostname)
		assert.Equal(t, "api-download", event.Payload.Name)
		assert.Equal(t, "/v1/files/dpdb_1/m1/dpdb_1.cif", event.Payload.URL)
		assert.Equal(t, map[string]string{
			"entry_id": "dpdb_1",
			"model_id": "m1",
			"filename": "dpdb_1.cif",
			"client":   "script",
			"agent":    "curl",
		}, event.Payload.Data)
		assert.Equal(t, senderUserAgent, <-userAgents)
	case <-time.After(2 * time.Second):
		require.Fail(t, "no event reached the analytics endpoint")
	}
}

func Test_should_send_nothing_when_website_id_is_empty(t *testing.T) {
	// given
	calls := make(chan struct{}, 1)
	umamiServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls <- struct{}{}
	}))
	defer umamiServer.Close()
	tracker := New(Config{Endpoint: umamiServer.URL})

	// when
	tracker.TrackDownload(Download{EntryID: "dpdb_1", UserAgent: "curl/8.7.1"})

	// then
	select {
	case <-calls:
		require.Fail(t, "a disabled tracker sent an event")
	case <-time.After(200 * time.Millisecond):
	}
}

func Test_should_send_nothing_when_request_comes_from_website_proxy(t *testing.T) {
	// given
	calls := make(chan struct{}, 1)
	umamiServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls <- struct{}{}
	}))
	defer umamiServer.Close()
	tracker := New(Config{Endpoint: umamiServer.URL, WebsiteID: "site-id"})

	// when
	tracker.TrackDownload(Download{EntryID: "dpdb_1", UserAgent: websiteProxyUserAgent})

	// then
	select {
	case <-calls:
		require.Fail(t, "a viewer proxy load was counted as a download")
	case <-time.After(200 * time.Millisecond):
	}
}

func Test_should_label_client_and_agent_when_user_agent_is_known(t *testing.T) {
	// given
	cases := map[string][2]string{
		"Mozilla/5.0 (Macintosh) Safari/605.1.15": {"browser", "Mozilla"},
		"python-requests/2.32.3":                  {"script", "python-requests"},
		"":                                        {"script", "unknown"},
	}

	for userAgent, want := range cases {
		// when
		client, agent := clientKind(userAgent), agentProduct(userAgent)

		// then
		assert.Equal(t, want[0], client, userAgent)
		assert.Equal(t, want[1], agent, userAgent)
	}
}
