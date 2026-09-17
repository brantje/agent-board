package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPermissionClientListsFiltersAndReplies(t *testing.T) {
	var reply struct {
		RequestID string
		Reply     string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/permission":
			_ = json.NewEncoder(w).Encode([]PermissionRequest{
				{ID: "per_1", SessionID: "ses_1", Permission: "agent_board_delegate"},
				{ID: "per_other", SessionID: "ses_other", Permission: "agent_board_delegate"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/permission/per_1/reply":
			var payload struct {
				Reply string `json:"reply"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			reply.RequestID = "per_1"
			reply.Reply = payload.Reply
			_ = json.NewEncoder(w).Encode(true)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	values, err := client.ListPermissions(context.Background(), "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID != "per_1" {
		t.Fatalf("permissions=%+v", values)
	}
	if err := client.ReplyPermission(context.Background(), "ses_1", "per_1", "once"); err != nil {
		t.Fatal(err)
	}
	if reply.RequestID != "per_1" || reply.Reply != "once" {
		t.Fatalf("reply=%+v", reply)
	}
}

func TestPermissionClientFallsBackToSessionResponse(t *testing.T) {
	var response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/permission/per_1/reply":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/session/ses_1/permissions/per_1":
			var payload struct {
				Response string `json:"response"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			response = payload.Response
			_ = json.NewEncoder(w).Encode(true)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ReplyPermission(context.Background(), "ses_1", "per_1", "reject"); err != nil {
		t.Fatal(err)
	}
	if response != "reject" {
		t.Fatalf("response=%q", response)
	}
}

func TestToolIDsSupportsWrappedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/experimental/tool/ids" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []string{"delegate_task", "set_issue_status"}})
	}))
	defer server.Close()
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := client.ToolIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "delegate_task" || ids[1] != "set_issue_status" {
		t.Fatalf("tool ids=%v", ids)
	}
}
