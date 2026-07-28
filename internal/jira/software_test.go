package jira

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSoftwareCapabilityDetection(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		want        CapabilityStatus
		wantErr     bool
		unsupported bool
	}{
		{name: "available", status: http.StatusOK, want: CapabilityAvailable},
		{name: "missing", status: http.StatusNotFound, want: CapabilityMissing, unsupported: true},
		{name: "permission denied", status: http.StatusForbidden, want: CapabilityUnknown, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/rest/agile/1.0/board" || r.URL.Query().Get("maxResults") != "1" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(test.status)
				if test.status == http.StatusOK {
					_, _ = w.Write([]byte(`{"values":[]}`))
				} else {
					_, _ = w.Write([]byte(`{"errorMessages":["software unavailable"]}`))
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
			got, err := client.SoftwareCapability(context.Background())
			if got != test.want || (err != nil) != test.wantErr {
				t.Fatalf("status = %q, err = %v", got, err)
			}
			requireErr := client.RequireSoftware(context.Background())
			var unsupported *UnsupportedCapabilityError
			if errors.As(requireErr, &unsupported) != test.unsupported {
				t.Fatalf("require error = %v", requireErr)
			}
		})
	}
}

func TestSoftwareBoardsPreserveScrumAndKanban(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		if r.URL.Path != "/rest/agile/1.0/board" ||
			r.URL.Query().Get("name") != "Team" ||
			r.URL.Query().Get("projectKeyOrId") != "ENG" ||
			r.URL.Query().Get("maxResults") != "2" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{
			"startAt":0,"maxResults":2,"total":2,"isLast":true,
			"values":[
				{"id":7,"name":"Team Scrum","type":"scrum"},
				{"id":8,"name":"Team Kanban","type":"kanban"}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.SoftwareBoards(context.Background(), "Team", "", "ENG", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Boards) != 2 ||
		result.Boards[0].Type != "scrum" ||
		result.Boards[1].Type != "kanban" ||
		result.Page.HasMore {
		t.Fatalf("result = %#v", result)
	}
}

func TestCloudSoftwareIssueReadsUseEnhancedTokenEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		call     func(*Client) (*SoftwareIssuePage, error)
	}{
		{
			name:     "board",
			resource: "/rest/software/1.0/board/7/issue",
			call: func(client *Client) (*SoftwareIssuePage, error) {
				return client.SoftwareBoardIssues(context.Background(), 7, false, SoftwareIssueOptions{
					MaxResults: 25, Cursor: "cloud-token", JQL: "status != Done", Fields: []string{"summary", "status"},
				})
			},
		},
		{
			name:     "backlog",
			resource: "/rest/software/1.0/board/7/backlog",
			call: func(client *Client) (*SoftwareIssuePage, error) {
				return client.SoftwareBoardIssues(context.Background(), 7, true, SoftwareIssueOptions{MaxResults: 25, Cursor: "cloud-token"})
			},
		},
		{
			name:     "sprint",
			resource: "/rest/software/1.0/sprint/9/issue",
			call: func(client *Client) (*SoftwareIssuePage, error) {
				return client.SoftwareSprintIssues(context.Background(), 9, SoftwareIssueOptions{MaxResults: 25, Cursor: "cloud-token"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/agile/1.0/board" {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				if r.URL.Path != test.resource ||
					r.URL.Query().Get("maxResults") != "25" ||
					r.URL.Query().Get("nextPageToken") != "cloud-token" {
					t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{
					"maxResults":25,
					"nextPageToken":"next-token",
					"isLast":false,
					"issues":[{"id":"1","key":"ENG-1","fields":{"summary":"One"}}]
				}`))
			}))
			defer server.Close()
			client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
			result, err := test.call(client)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Issues) != 1 || result.Page.Next != "next-token" ||
				!result.Page.HasMore || result.Page.Total != nil {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestDataCenterSoftwareIssueReadUsesOffsetEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		if r.URL.Path != "/rest/agile/1.0/board/7/backlog" ||
			r.URL.Query().Get("startAt") != "20" ||
			r.URL.Query().Get("maxResults") != "2" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{
			"startAt":20,"maxResults":2,"total":23,
			"issues":[{"id":"21","key":"ENG-21","fields":{}},{"id":"22","key":"ENG-22","fields":{}}]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.SoftwareBoardIssues(context.Background(), 7, true, SoftwareIssueOptions{MaxResults: 2, Cursor: "20"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Page.Next != "22" || !result.Page.HasMore ||
		result.Page.Total == nil || *result.Page.Total != 23 {
		t.Fatalf("page = %#v", result.Page)
	}
}

func TestSoftwareSprintListAndGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		switch r.URL.Path {
		case "/rest/agile/1.0/board/7/sprint":
			if r.URL.Query().Get("state") != "active,future" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":2,"isLast":true,"values":[{"id":9,"state":"active","name":"Sprint 9"}]}`))
		case "/rest/agile/1.0/sprint/9":
			_, _ = w.Write([]byte(`{"id":9,"state":"active","name":"Sprint 9","originBoardId":7}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	page, err := client.SoftwareBoardSprints(context.Background(), 7, "active,future", 0, 2)
	if err != nil || len(page.Sprints) != 1 || page.Sprints[0].ID != 9 {
		t.Fatalf("page = %#v, err = %v", page, err)
	}
	sprint, err := client.SoftwareSprint(context.Background(), 9)
	if err != nil || sprint.OriginBoardID != 7 {
		t.Fatalf("sprint = %#v, err = %v", sprint, err)
	}
}

func TestSoftwareWritesPreserveOrderingAndExactPayloads(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		call        func(*Client) (*SoftwareWriteReceipt, error)
		wantBody    map[string]any
		multiStatus bool
		deployment  Deployment
	}{
		{
			name:   "move sprint",
			method: http.MethodPost,
			path:   "/rest/agile/1.0/sprint/9/issue",
			call: func(client *Client) (*SoftwareWriteReceipt, error) {
				return client.MoveIssuesToSprint(context.Background(), 9, []string{"ENG-2", "ENG-1"}, "ENG-3", "", 10521)
			},
			wantBody: map[string]any{"issues": []any{"ENG-2", "ENG-1"}, "rankBeforeIssue": "ENG-3", "rankCustomFieldId": json.Number("10521")},
		},
		{
			name:   "move backlog",
			method: http.MethodPost,
			path:   "/rest/agile/1.0/backlog/7/issue",
			call: func(client *Client) (*SoftwareWriteReceipt, error) {
				return client.MoveIssuesToBacklog(context.Background(), 7, []string{"ENG-2", "ENG-1"})
			},
			wantBody:   map[string]any{"issues": []any{"ENG-2", "ENG-1"}},
			deployment: DeploymentCloud,
		},
		{
			name:   "rank partial",
			method: http.MethodPut,
			path:   "/rest/agile/1.0/issue/rank",
			call: func(client *Client) (*SoftwareWriteReceipt, error) {
				return client.RankIssues(context.Background(), []string{"ENG-2", "ENG-1"}, "", "ENG-3", 0)
			},
			wantBody:    map[string]any{"issues": []any{"ENG-2", "ENG-1"}, "rankAfterIssue": "ENG-3"},
			multiStatus: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/agile/1.0/board" {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				if r.Method != test.method || r.URL.Path != test.path {
					t.Fatalf("request = %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				decoder := json.NewDecoder(r.Body)
				decoder.UseNumber()
				if err := decoder.Decode(&body); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(body, test.wantBody) {
					t.Fatalf("body = %#v, want %#v", body, test.wantBody)
				}
				if test.multiStatus {
					w.WriteHeader(http.StatusMultiStatus)
					_, _ = w.Write([]byte(`{"entries":[{"issueId":"ENG-1","errors":["rank failed"]}]}`))
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			deployment := test.deployment
			if deployment == "" {
				deployment = DeploymentDataCenter
			}
			client := NewClient(server.URL, "token", "", deployment, time.Second)
			receipt, err := test.call(client)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(receipt.Issues, []string{"ENG-2", "ENG-1"}) {
				t.Fatalf("issues = %#v", receipt.Issues)
			}
			if test.multiStatus {
				if !receipt.Partial || receipt.Accepted ||
					!strings.Contains(string(receipt.Details), "rank failed") {
					t.Fatalf("receipt = %#v", receipt)
				}
			} else if receipt.Partial || !receipt.Accepted {
				t.Fatalf("receipt = %#v", receipt)
			}
		})
	}
}

func TestEstimateIssueUsesBoardScopedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		if r.Method != http.MethodPut ||
			r.URL.Path != "/rest/agile/1.0/issue/ENG-1/estimation" ||
			r.URL.Query().Get("boardId") != "7" {
			t.Fatalf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["value"] != "8.0" {
			t.Fatalf("body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"fieldId":"customfield_10016","value":"8.0"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.EstimateIssue(context.Background(), "ENG-1", 7, "8.0")
	if err != nil || !strings.Contains(string(result), "customfield_10016") {
		t.Fatalf("result = %s, err = %v", result, err)
	}
}

func TestSoftwareWriteUsesHTTP207ForPartialState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.RankIssues(context.Background(), []string{"ENG-1"}, "", "ENG-2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Partial || result.Accepted {
		t.Fatalf("result = %#v", result)
	}
}

func TestSoftwareWriteValidationHappensBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)

	tooMany := make([]string, SoftwareWriteIssueLimit+1)
	for i := range tooMany {
		tooMany[i] = "ENG-1"
	}
	checks := []func() error{
		func() error {
			_, err := client.MoveIssuesToSprint(context.Background(), 9, tooMany, "ENG-2", "", 0)
			return err
		},
		func() error {
			_, err := client.RankIssues(context.Background(), []string{"ENG-1"}, "ENG-2", "ENG-3", 0)
			return err
		},
		func() error {
			_, err := client.EstimateIssue(context.Background(), "ENG-1", 0, "8")
			return err
		},
	}
	for i, check := range checks {
		if err := check(); err == nil {
			t.Fatalf("check %d returned nil", i)
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestDataCenterRejectsBoardScopedBacklogBeforeMutation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	_, err := client.MoveIssuesToBacklog(context.Background(), 7, []string{"ENG-1"})
	var unsupported *UnsupportedCapabilityError
	if !errors.As(err, &unsupported) ||
		unsupported.Capability != "board-scoped backlog move" ||
		requests != 0 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
}
