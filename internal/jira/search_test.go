package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCloudSearchUsesEnhancedPostAndNormalizesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/jql" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			JQL               string   `json:"jql"`
			Fields            []string `json:"fields"`
			MaxResults        int      `json:"maxResults"`
			NextPageToken     string   `json:"nextPageToken"`
			ReconcileIssueIDs []int64  `json:"reconcileIssues"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.JQL != "project = ENG AND summary ~ \"a very long query\"" {
			t.Fatalf("JQL = %q", body.JQL)
		}
		if !reflect.DeepEqual(body.Fields, []string{"summary", "description"}) {
			t.Fatalf("Fields = %#v", body.Fields)
		}
		if body.MaxResults != 25 || body.NextPageToken != "cloud-token" {
			t.Fatalf("page request = %#v", body)
		}
		if !reflect.DeepEqual(body.ReconcileIssueIDs, []int64{10001, 10002}) {
			t.Fatalf("ReconcileIssueIDs = %#v", body.ReconcileIssueIDs)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issues": [{
				"id": "10001",
				"key": "ENG-1",
				"fields": {
					"summary": "Test",
					"description": {
						"type": "doc",
						"version": 1,
						"content": [{
							"type": "paragraph",
							"content": [
								{"type": "text", "text": "Hello "},
								{"type": "text", "text": "world"}
							]
						}]
					}
				}
			}],
			"nextPageToken": "next-cloud-token",
			"isLast": false
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	result, err := client.Search(context.Background(), SearchOptions{
		JQL:               `project = ENG AND summary ~ "a very long query"`,
		Fields:            []string{"summary", "description"},
		MaxResults:        25,
		Cursor:            "cloud-token",
		ReconcileIssueIDs: []int64{10001, 10002},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 1 || result.Issues[0].Fields.Description.PlainText() != "Hello world" {
		t.Fatalf("Issues = %#v", result.Issues)
	}
	if result.Page.Next != "next-cloud-token" || !result.Page.HasMore || result.Page.Pages != 1 {
		t.Fatalf("Page = %#v", result.Page)
	}
	if result.Page.Total != nil {
		t.Fatalf("Cloud total = %v, want nil", result.Page.Total)
	}
}

func TestDataCenterSearchUsesPostAndNormalizesOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/2/search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			JQL        string   `json:"jql"`
			Fields     []string `json:"fields"`
			MaxResults int      `json:"maxResults"`
			StartAt    int      `json:"startAt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.StartAt != 20 || body.MaxResults != 2 {
			t.Fatalf("body = %#v", body)
		}
		_, _ = w.Write([]byte(`{
			"startAt": 20,
			"maxResults": 2,
			"total": 23,
			"issues": [
				{"id":"21","key":"ENG-21","fields":{"summary":"Twenty one"}},
				{"id":"22","key":"ENG-22","fields":{"summary":"Twenty two"}}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.Search(context.Background(), SearchOptions{
		JQL:        "project = ENG",
		Fields:     []string{"summary"},
		MaxResults: 2,
		Cursor:     "20",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Page.Next != "22" || !result.Page.HasMore || result.Page.Total == nil || *result.Page.Total != 23 {
		t.Fatalf("Page = %#v", result.Page)
	}
}

func TestSearchAllStopsAtHardLimit(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			MaxResults    int    `json:"maxResults"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch requests {
		case 1:
			if body.MaxResults != 2 || body.NextPageToken != "" {
				t.Fatalf("first body = %#v", body)
			}
			_, _ = w.Write([]byte(`{
				"issues":[
					{"id":"1","key":"ENG-1","fields":{"summary":"One"}},
					{"id":"2","key":"ENG-2","fields":{"summary":"Two"}}
				],
				"nextPageToken":"page-2",
				"isLast":false
			}`))
		case 2:
			if body.MaxResults != 1 || body.NextPageToken != "page-2" {
				t.Fatalf("second body = %#v", body)
			}
			_, _ = w.Write([]byte(`{
				"issues":[{"id":"3","key":"ENG-3","fields":{"summary":"Three"}}],
				"nextPageToken":"page-3",
				"isLast":false
			}`))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	result, err := client.SearchAll(context.Background(), SearchOptions{
		JQL:        "project = ENG",
		Fields:     []string{"summary"},
		MaxResults: 2,
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(result.Issues) != 3 {
		t.Fatalf("requests = %d, issues = %d", requests, len(result.Issues))
	}
	if result.Page.Returned != 3 || result.Page.Limit != 3 || result.Page.Pages != 2 {
		t.Fatalf("Page = %#v", result.Page)
	}
	if !result.Page.HasMore || result.Page.Next != "page-3" {
		t.Fatalf("Page = %#v", result.Page)
	}
}

func TestDataCenterSearchAllStopsAtHardLimit(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			MaxResults int `json:"maxResults"`
			StartAt    int `json:"startAt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch requests {
		case 1:
			if body.MaxResults != 2 || body.StartAt != 0 {
				t.Fatalf("first body = %#v", body)
			}
			_, _ = w.Write([]byte(`{
				"startAt":0,
				"maxResults":2,
				"total":4,
				"issues":[
					{"id":"1","key":"ENG-1","fields":{"summary":"One"}},
					{"id":"2","key":"ENG-2","fields":{"summary":"Two"}}
				]
			}`))
		case 2:
			if body.MaxResults != 1 || body.StartAt != 2 {
				t.Fatalf("second body = %#v", body)
			}
			_, _ = w.Write([]byte(`{
				"startAt":2,
				"maxResults":1,
				"total":4,
				"issues":[{"id":"3","key":"ENG-3","fields":{"summary":"Three"}}]
			}`))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.SearchAll(context.Background(), SearchOptions{
		JQL:        "project = ENG",
		Fields:     []string{"summary"},
		MaxResults: 2,
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	gotKeys := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		gotKeys = append(gotKeys, issue.Key)
	}
	wantKeys := []string{"ENG-1", "ENG-2", "ENG-3"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("keys = %#v, want normalized collection %#v", gotKeys, wantKeys)
	}
	if requests != 2 ||
		result.Page.Returned != 3 ||
		result.Page.Limit != 3 ||
		result.Page.Pages != 2 ||
		!result.Page.HasMore ||
		result.Page.Next != "3" {
		t.Fatalf("requests = %d, page = %#v", requests, result.Page)
	}
}

func TestSearchAllReturnsZeroResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":10,"total":0,"issues":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.SearchAll(context.Background(), SearchOptions{
		JQL:        "project = EMPTY",
		Fields:     []string{"summary"},
		MaxResults: 10,
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 || result.Page.Pages != 1 || result.Page.HasMore {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchAllRejectsRepeatedCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"issues":[{"id":"1","key":"ENG-1","fields":{"summary":"One"}}],
			"nextPageToken":"same",
			"isLast":false
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	_, err := client.SearchAll(context.Background(), SearchOptions{
		JQL:        "project = ENG",
		Fields:     []string{"summary"},
		MaxResults: 1,
	}, 3)
	if err == nil || !strings.Contains(err.Error(), "repeated continuation cursor") {
		t.Fatalf("error = %v", err)
	}
}

func TestDataCenterSearchRejectsInvalidCursorAndReconciliationLocally(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)

	tests := map[string]struct {
		options SearchOptions
		assert  func(error) bool
	}{
		"cursor": {
			options: SearchOptions{
				JQL: "project = ENG", Fields: []string{"summary"}, MaxResults: 10, Cursor: "opaque",
			},
			assert: func(err error) bool {
				var validationErr *ValidationError
				return errors.As(err, &validationErr) && validationErr.Field == "cursor"
			},
		},
		"reconcile": {
			options: SearchOptions{
				JQL: "project = ENG", Fields: []string{"summary"}, MaxResults: 10, ReconcileIssueIDs: []int64{10001},
			},
			assert: func(err error) bool {
				var capabilityErr *UnsupportedCapabilityError
				return errors.As(err, &capabilityErr) && capabilityErr.Capability == "search reconciliation"
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := client.Search(context.Background(), test.options)
			if !test.assert(err) {
				t.Fatalf("error = %T %v", err, err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestSearchValidatesReconciliationBudget(t *testing.T) {
	ids := make([]int64, 51)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	client := NewClient("https://jira.example.com", "token", "agent@example.com", DeploymentCloud, time.Second)
	_, err := client.Search(context.Background(), SearchOptions{
		JQL: "project = ENG", Fields: []string{"summary"}, MaxResults: 10, ReconcileIssueIDs: ids,
	})
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "reconcileIssues" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestSearchReturnsJiraValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["invalid JQL"]}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	_, err := client.Search(context.Background(), SearchOptions{
		JQL: "broken", Fields: []string{"summary"}, MaxResults: 10,
	})
	var jiraErr *Error
	if !errors.As(err, &jiraErr) || jiraErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestRichTextPreservesADFJSON(t *testing.T) {
	input := []byte(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Hello"}]}]}`)
	var text RichText
	if err := json.Unmarshal(input, &text); err != nil {
		t.Fatal(err)
	}
	output, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(input, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output = %s", output)
	}
	if text.PlainText() != "Hello" {
		t.Fatalf("PlainText = %q", text.PlainText())
	}
}

func ExampleSearchPage_cursor() {
	page := SearchPage{Next: "opaque-token", HasMore: true}
	fmt.Println(page.Next)
	// Output: opaque-token
}
