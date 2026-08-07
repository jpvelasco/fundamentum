package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientGet(t *testing.T) {
	srv, _ := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong Authorization header: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		var out any
		_ = json.Unmarshal([]byte(`{"id":1}`), &out)
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	c := NewClient("test-token", false).WithBaseURL(srv.URL)
	resp, err := c.get("/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestClientBase_DefaultFallback(t *testing.T) {
	c := &Client{Token: "t", Verbose: false, baseURL: ""}
	got := c.base()
	if got != defaultBase {
		t.Errorf("expected %s, got %s", defaultBase, got)
	}
}

func TestWithBaseURL_RejectsNonHTTPS(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantSet bool
	}{
		{"empty url", "", false},
		{"invalid url", "not a url", false},
		{"http scheme", "http://evil.com", false},
		{"ftp scheme", "ftp://evil.com", false},
		{"https scheme", "https://api.github.com", true},
		{"localhost", "http://localhost:8080", true},
		{"127.0.0.1", "http://127.0.0.1:8080", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := (&Client{Token: "t", Verbose: false}).WithBaseURL(tt.url)
			if tt.wantSet && c.baseURL == "" {
				t.Error("expected baseURL to be set, got empty")
			}
			if !tt.wantSet && c.baseURL != "" {
				t.Errorf("expected baseURL to remain empty, got %q", c.baseURL)
			}
		})
	}
}

func TestClientPatch(t *testing.T) {
	srv, _ := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := NewClient("test-token", false).WithBaseURL(srv.URL)
	resp, err := c.patch("/", map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// Given an unexpected HTTP status, expectStatus must return an error that
// unwraps to *HTTPError carrying the status code, so callers can classify
// errors structurally (errors.As) instead of matching on message text.
func TestExpectStatus_ReturnsHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
	}{
		{"conflict response carries 409", http.StatusConflict, `{"message":"rule violations"}`, http.StatusConflict},
		{"forbidden response carries 403", http.StatusForbidden, `{"message":"Forbidden"}`, http.StatusForbidden},
		{"server error carries 500", http.StatusInternalServerError, "boom", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.status,
				Status:     http.StatusText(tt.status),
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}
			err := expectStatus("test action", resp, http.StatusOK)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var he *HTTPError
			if !errors.As(err, &he) {
				t.Fatalf("expected error to unwrap to *HTTPError, got %T", err)
			}
			if he.StatusCode != tt.wantStatus {
				t.Errorf("expected StatusCode %d, got %d", tt.wantStatus, he.StatusCode)
			}
		})
	}
}

// newZeroDelayClient returns a Client pointed at the given base URL whose
// retry backoff is zeroed out so retry-loop tests run without sleeping.
func newZeroDelayClient(baseURL string) *Client {
	c := NewClient("t", false).WithBaseURL(baseURL)
	c.retryDelay = func(int, time.Duration) time.Duration { return 0 }
	return c
}

// countingServer returns a server that responds per the given sequence and
// records how many requests it received.
func countingServer(responses ...int) (*httptest.Server, *int) {
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := responses[0]
		if count > 0 && count < len(responses) {
			status = responses[count]
		}
		count++
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(status)
	}))
	return srv, &count
}

// Given a transient failure (429) followed by success, the client must retry
// and return the successful response.
func TestDo_RetriesTransientThenSucceeds(t *testing.T) {
	srv, count := countingServer(http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusOK)
	defer srv.Close()

	resp, err := newZeroDelayClient(srv.URL).get("/ok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected final status 200, got %d", resp.StatusCode)
	}
	if *count != 3 {
		t.Errorf("expected 3 attempts, got %d", *count)
	}
}

// Given a persistent 429, the client must stop after maxAttempts and hand
// back the last response so callers classify it via expectStatus.
func TestDo_GivesUpAfterMaxAttempts(t *testing.T) {
	srv, count := countingServer(http.StatusTooManyRequests)
	defer srv.Close()

	resp, err := newZeroDelayClient(srv.URL).get("/ratelimited")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *count != maxAttempts {
		t.Errorf("expected %d attempts, got %d", maxAttempts, *count)
	}
	statusErr := expectStatus("get /ratelimited", resp, http.StatusOK)
	var he *HTTPError
	if !errors.As(statusErr, &he) || he.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected last response to classify as 429, got: %v", statusErr)
	}
}

// Given a server-side 503 followed by success, the client must retry.
func TestDo_RetriesServerError(t *testing.T) {
	srv, count := countingServer(http.StatusServiceUnavailable, http.StatusOK)
	defer srv.Close()

	resp, err := newZeroDelayClient(srv.URL).get("/flaky")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected final status 200, got %d", resp.StatusCode)
	}
	if *count != 2 {
		t.Errorf("expected 2 attempts, got %d", *count)
	}
}

// Given a client error (401), the request must NOT be retried.
func TestDo_NoRetryOnClientError(t *testing.T) {
	srv, count := countingServer(http.StatusUnauthorized)
	defer srv.Close()

	resp, err := newZeroDelayClient(srv.URL).get("/unauthorized")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	if *count != 1 {
		t.Errorf("expected 1 attempt, got %d", *count)
	}
}

// Given a 403 with X-RateLimit-Remaining: 0 (rate-limit exhaustion), the
// request must be retried; a plain 403 must not.
func TestRetryableStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		rateHeader string
		want       bool
	}{
		{"429 rate limit", http.StatusTooManyRequests, "", true},
		{"500 internal", http.StatusInternalServerError, "", true},
		{"502 bad gateway", http.StatusBadGateway, "", true},
		{"503 unavailable", http.StatusServiceUnavailable, "", true},
		{"504 gateway timeout", http.StatusGatewayTimeout, "", true},
		{"403 with exhausted rate limit", http.StatusForbidden, "0", true},
		{"403 without rate-limit header", http.StatusForbidden, "", false},
		{"401 unauthorized", http.StatusUnauthorized, "", false},
		{"404 not found", http.StatusNotFound, "", false},
		{"409 conflict", http.StatusConflict, "", false},
		{"422 unprocessable", http.StatusUnprocessableEntity, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.status}
			resp.Header = http.Header{}
			if tt.rateHeader != "" {
				resp.Header.Set("X-RateLimit-Remaining", tt.rateHeader)
			}
			if got := retryableStatus(resp); got != tt.want {
				t.Errorf("retryableStatus(%d) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// Given a 429 with a Retry-After header, the backoff must honor it rather
// than falling back to exponential jitter.
func TestRetryDelay_HonorsRetryAfter(t *testing.T) {
	var gotRetryAfter time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("t", false).WithBaseURL(srv.URL)
	c.retryDelay = func(_ int, retryAfter time.Duration) time.Duration {
		gotRetryAfter = retryAfter
		return 0
	}
	_, err := c.get("/ratelimited")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRetryAfter != 7*time.Second {
		t.Errorf("expected retry delay to honor Retry-After: 7s, got %s", gotRetryAfter)
	}
}

// Given a transport-level failure, the request must fail immediately — no
// retry loop, no misleading sleep.
func TestDo_NoRetryOnTransportError(t *testing.T) {
	c := newErroringClient()
	c.retryDelay = func(int, time.Duration) time.Duration { return 0 }
	_, err := c.get("/down")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRepoPath_SafeEscaping(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		repo  string
		want  string
	}{
		{"simple", "owner", "repo", "/repos/owner/repo"},
		{"slash in owner", "owner/repo", "bad", "/repos/owner%2Frepo/bad"},
		{"slash in repo", "owner", "repo/name", "/repos/owner/repo%2Fname"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoPath(tt.owner, tt.repo)
			if got != tt.want {
				t.Errorf("repoPath(%q, %q) = %q, want %q", tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}
