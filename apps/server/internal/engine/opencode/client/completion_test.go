package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestAssistantErrorReadsNewestDurableFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/ses_test/message" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Fatalf("message limit=%q want 1", got)
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{
				"info": map[string]any{
					"role": "assistant",
					"error": map[string]any{
						"name": "ProviderAuthError",
						"data": map[string]any{"providerID": "openrouter", "message": "authentication failed"},
					},
				},
				"parts": []any{},
			},
		})
	}))
	defer server.Close()

	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	message, failed, err := client.LatestAssistantError(context.Background(), "ses_test")
	if err != nil {
		t.Fatal(err)
	}
	if !failed || message != "authentication failed" {
		t.Fatalf("LatestAssistantError() = %q, %v; want authentication failed, true", message, failed)
	}
}

func TestLatestAssistantErrorIgnoresSuccessfulOrUnavailableHistory(t *testing.T) {
	t.Run("successful assistant", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"info": map[string]any{"role": "assistant"}, "parts": []any{}},
			})
		}))
		defer server.Close()
		client, err := New(server.Client(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if message, failed, err := client.LatestAssistantError(context.Background(), "ses_test"); err != nil || failed || message != "" {
			t.Fatalf("LatestAssistantError() = %q, %v, %v; want empty, false, nil", message, failed, err)
		}
	})

	t.Run("compatibility route unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()
		client, err := New(server.Client(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if message, failed, err := client.LatestAssistantError(context.Background(), "ses_test"); err != nil || failed || message != "" {
			t.Fatalf("LatestAssistantError() = %q, %v, %v; want empty, false, nil", message, failed, err)
		}
	})
}

func TestDecodeNativeErrorMessageFallsBackToName(t *testing.T) {
	message, err := DecodeNativeErrorMessage(json.RawMessage(`{"name":"APIError","data":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if message != "APIError" {
		t.Fatalf("DecodeNativeErrorMessage()=%q want APIError", message)
	}
}

func TestClientListMessages(t *testing.T) {
	t.Run("requires session id", func(t *testing.T) {
		client, err := New(http.DefaultClient, "http://opencode.example")
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ListMessages(context.Background(), "  ")
		if err == nil || !strings.Contains(err.Error(), "session id is required") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("missing history is empty", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(server.Close)
		client, err := New(server.Client(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		messages, err := client.ListMessages(context.Background(), "ses_missing")
		if err != nil || messages != nil {
			t.Fatalf("messages=%v err=%v", messages, err)
		}
	})

	t.Run("wrapped data", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/session/ses_1/message" {
				http.NotFound(w, r)
				return
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{map[string]any{"info": map[string]any{"id": "msg_wrapped"}, "parts": []any{}}},
			})
		}))
		t.Cleanup(server.Close)
		client, err := New(server.Client(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		messages, err := client.ListMessages(context.Background(), "ses_1")
		if err != nil || len(messages) != 1 {
			t.Fatalf("messages=%+v err=%v", messages, err)
		}
		var info struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(messages[0].Info, &info); err != nil || info.ID != "msg_wrapped" {
			t.Fatalf("info=%+v err=%v", info, err)
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK, "oops")
		}))
		t.Cleanup(server.Close)
		client, err := New(server.Client(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ListMessages(context.Background(), "ses_1")
		if err == nil || !strings.Contains(err.Error(), "decode session messages") {
			t.Fatalf("err=%v", err)
		}
	})
}
