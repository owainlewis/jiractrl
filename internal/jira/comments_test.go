package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCommentsUseDeploymentAPIAndPreserveVisibility(t *testing.T) {
	for _, test := range []struct {
		name       string
		deployment Deployment
		prefix     string
	}{
		{name: "cloud", deployment: DeploymentCloud, prefix: "/rest/api/3"},
		{name: "data_center", deployment: DeploymentDataCenter, prefix: "/rest/api/2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.RequestURI())
				switch r.Method {
				case http.MethodGet:
					_, _ = w.Write([]byte(`{"startAt":2,"maxResults":1,"total":4,"comments":[{"id":"12","body":"restricted","visibility":{"type":"role","value":"Developers","identifier":"10001"}}]}`))
				case http.MethodPost, http.MethodPut:
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					visibility := body["visibility"].(map[string]any)
					_, _ = w.Write([]byte(`{"id":"12","body":"restricted","visibility":{"type":"` + visibility["type"].(string) + `","value":"` + visibility["value"].(string) + `"}}`))
				case http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "", test.deployment, time.Second)
			ctx := context.Background()
			page, err := client.Comments(ctx, "ENG-1", 2, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !page.Page.HasMore || page.Page.Next != 3 ||
				page.Comments[0].Visibility.Value != "Developers" ||
				page.Comments[0].Visibility.Identifier != "10001" {
				t.Fatalf("page = %#v", page)
			}
			payload := map[string]any{
				"body":       "restricted",
				"visibility": map[string]any{"type": "role", "value": "Developers"},
			}
			if _, err := client.AddCommentWithPayload(ctx, "ENG-1", payload); err != nil {
				t.Fatal(err)
			}
			if _, err := client.UpdateCommentWithPayload(ctx, "ENG-1", "12", payload); err != nil {
				t.Fatal(err)
			}
			if err := client.RemoveComment(ctx, "ENG-1", "12"); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(requests, "\n")
			for _, request := range []string{
				"GET " + test.prefix + "/issue/ENG-1/comment?maxResults=1&startAt=2",
				"POST " + test.prefix + "/issue/ENG-1/comment",
				"PUT " + test.prefix + "/issue/ENG-1/comment/12",
				"DELETE " + test.prefix + "/issue/ENG-1/comment/12",
			} {
				if !strings.Contains(joined, request) {
					t.Fatalf("requests missing %q:\n%s", request, joined)
				}
			}
		})
	}
}

func TestRemoveCommentIsNeverRetried(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentCloud, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})
	err := client.RemoveComment(context.Background(), "ENG-1", "12")
	if err == nil {
		t.Fatal("expected error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
