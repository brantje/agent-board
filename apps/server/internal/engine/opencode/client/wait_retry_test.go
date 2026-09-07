package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWaitSessionRetriesOnlySessionWaitReadiness(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session/ses_1/wait" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		attempts++
		if attempts < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"_tag":"ServiceUnavailableError","message":"Session wait is not available yet","service":"session.wait"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.WaitSession(context.Background(), "ses_1"); err != nil {
		t.Fatalf("WaitSession() error=%v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d want 3", attempts)
	}
}

func TestWaitSessionDoesNotRetryOtherServiceUnavailableErrors(t *testing.T) {
	for _, body := range []string{
		`{"_tag":"ServiceUnavailableError","service":"session.compact"}`,
		`{"_tag":"OtherError","service":"session.wait"}`,
		`not-json`,
	} {
		t.Run(body, func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			native, err := New(server.Client(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			err = native.WaitSession(context.Background(), "ses_1")
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("WaitSession() error=%v", err)
			}
			if attempts != 1 {
				t.Fatalf("attempts=%d want 1", attempts)
			}
		})
	}
}

func TestWaitSessionReadinessRetryHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"_tag":"ServiceUnavailableError","service":"session.wait"}`))
	}))
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := native.WaitSession(ctx, "ses_1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitSession() error=%v", err)
	}
}

func TestIsWaitOperationUnavailableRejectsNonHTTPAndWrongStatus(t *testing.T) {
	if isWaitOperationUnavailable(errors.New("transport failed")) {
		t.Fatal("transport error unexpectedly treated as wait readiness")
	}
	if isWaitOperationUnavailable(&HTTPError{StatusCode: http.StatusBadGateway, Body: `{"_tag":"ServiceUnavailableError","service":"session.wait"}`}) {
		t.Fatal("wrong status unexpectedly treated as wait readiness")
	}
	if !isWaitOperationUnavailable(&HTTPError{StatusCode: http.StatusServiceUnavailable, Body: `{"_tag":"ServiceUnavailableError","service":"session.wait"}`}) {
		t.Fatal("typed session.wait readiness was not recognized")
	}
	if err := (&Client{}).WaitSession(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "session id") {
		t.Fatalf("empty session id error=%v", err)
	}
}
