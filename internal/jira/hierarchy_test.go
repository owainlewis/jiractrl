package jira

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveAssignableUserCloudRejectsAmbiguousDisplayName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/user/assignable/search" ||
			r.URL.Query().Get("issueKey") != "ENG-1" ||
			r.URL.Query().Get("query") != "Alex Smith" {
			t.Fatalf("request = %s", r.URL.RequestURI())
		}
		_, _ = w.Write([]byte(`[
			{"accountId":"a1","displayName":"Alex Smith","active":true},
			{"accountId":"a2","displayName":"Alex Smith","active":true}
		]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	_, err := client.ResolveAssignableUser(context.Background(), "ENG-1", "Alex Smith")
	var ambiguous *AmbiguousUserError
	if !errors.As(err, &ambiguous) || len(ambiguous.Candidates) != 2 {
		t.Fatalf("error = %#v", err)
	}
}

func TestResolveAssignableUserRejectsApproximateMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"accountId":"a1","displayName":"Alexandra Smith","active":true}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	_, err := client.ResolveAssignableUser(context.Background(), "ENG-1", "Alex")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "user" {
		t.Fatalf("error = %#v", err)
	}
}

func TestResolveAssignableUserDataCenterUsesExactUsername(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("username") != "john" || r.URL.Query().Get("query") != "" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"name":"john","key":"jdoe","displayName":"John Doe","active":true}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	user, err := client.ResolveAssignableUser(context.Background(), "ENG-1", "john")
	if err != nil || user.Name != "john" {
		t.Fatalf("user = %#v, err = %v", user, err)
	}
}

func TestResolveAssignableAccountIDUsesDedicatedCloudFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("accountId") != "a1" ||
			r.URL.Query().Get("issueKey") != "ENG-1" ||
			r.URL.Query().Get("query") != "" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"accountId":"a1","displayName":"Alex","active":true}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	user, err := client.ResolveAssignableAccountID(context.Background(), "ENG-1", "a1")
	if err != nil || user.AccountID != "a1" {
		t.Fatalf("user = %#v, err = %v", user, err)
	}
}

func TestAssignIssueUsesDeploymentIdentityAndUnassignShape(t *testing.T) {
	tests := []struct {
		name       string
		deployment Deployment
		accountID  string
		username   string
		unassign   bool
		path       string
		field      string
		want       any
	}{
		{name: "cloud account", deployment: DeploymentCloud, accountID: "a1", path: "/rest/api/3/issue/ENG-1/assignee", field: "accountId", want: "a1"},
		{name: "cloud unassign", deployment: DeploymentCloud, unassign: true, path: "/rest/api/3/issue/ENG-1/assignee", field: "accountId", want: nil},
		{name: "data center user", deployment: DeploymentDataCenter, username: "john", path: "/rest/api/2/issue/ENG-1/assignee", field: "name", want: "john"},
		{name: "data center unassign", deployment: DeploymentDataCenter, unassign: true, path: "/rest/api/2/issue/ENG-1/assignee", field: "name", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodPut || r.URL.Path != test.path {
					t.Fatalf("request = %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				value, exists := body[test.field]
				if !exists || value != test.want {
					t.Fatalf("body = %#v", body)
				}
				if len(body) != 1 {
					t.Fatalf("body = %#v", body)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "agent@example.com", test.deployment, time.Second)
			receipt, err := client.AssignIssue(context.Background(), "ENG-1", test.accountID, test.username, test.unassign)
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || receipt.Unassigned != test.unassign {
				t.Fatalf("requests = %d, receipt = %#v", requests, receipt)
			}
		})
	}
}

func TestAssignmentMutationIsNeverRetried(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})
	if _, err := client.AssignIssue(context.Background(), "ENG-1", "a1", "", false); err == nil {
		t.Fatal("expected error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestDataCenterParentUpdateIsUnsupported(t *testing.T) {
	client := NewClient("https://jira.example.com", "token", "", DeploymentDataCenter, time.Second)
	err := client.ValidateParentUpdate(context.Background())
	var unsupported *UnsupportedCapabilityError
	if !errors.As(err, &unsupported) ||
		unsupported.Deployment != DeploymentDataCenter ||
		!strings.Contains(unsupported.Capability, "parent") {
		t.Fatalf("error = %#v", err)
	}
}
