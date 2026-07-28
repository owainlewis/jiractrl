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

func TestIssueLinksNormalizeInwardAndOutwardDirections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issueLinkType":
			_, _ = w.Write([]byte(`{"issueLinkTypes":[{"id":"1","name":"Blocks","inward":"is blocked by","outward":"blocks"}]}`))
		case "/rest/api/3/issue/ENG-1":
			if r.URL.Query().Get("fields") != "issuelinks" {
				t.Fatalf("fields = %q", r.URL.Query().Get("fields"))
			}
			_, _ = w.Write([]byte(`{"fields":{"issuelinks":[
				{"id":"10","type":{"id":"1","name":"Blocks","inward":"is blocked by","outward":"blocks"},"outwardIssue":{"id":"2","key":"ENG-2"}},
				{"id":"11","type":{"id":"1","name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"id":"3","key":"ENG-3"}}
			]}}`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	types, err := client.IssueLinkTypes(context.Background())
	if err != nil || len(types) != 1 || types[0].Outward != "blocks" {
		t.Fatalf("types = %#v, err = %v", types, err)
	}
	links, err := client.IssueLinks(context.Background(), "ENG-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 ||
		links[0].Direction != "outward" || links[0].Relation != "blocks" || links[0].Issue.Key != "ENG-2" ||
		links[1].Direction != "inward" || links[1].Relation != "is blocked by" || links[1].Issue.Key != "ENG-3" {
		t.Fatalf("links = %#v", links)
	}
}

func TestAddIssueLinkReportsDocumentedDuplicateSemantics(t *testing.T) {
	requests := 0
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/2/issueLink" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})
	receipt, err := client.AddIssueLink(context.Background(), "Duplicate", "ENG-1", "ENG-2")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !receipt.Accepted || !receipt.DuplicateRequestsSucceed || receipt.ServerReturnsCreatedLinkID {
		t.Fatalf("requests = %d, receipt = %#v", requests, receipt)
	}
	encoded, _ := json.Marshal(payload)
	for _, fragment := range []string{
		`"type":{"name":"Duplicate"}`,
		`"outwardIssue":{"key":"ENG-1"}`,
		`"inwardIssue":{"key":"ENG-2"}`,
	} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("payload missing %s: %s", fragment, encoded)
		}
	}
}

func TestRemoveIssueLinkIsNeverRetried(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentCloud, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})
	if err := client.RemoveIssueLink(context.Background(), "10"); err == nil {
		t.Fatal("expected error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
