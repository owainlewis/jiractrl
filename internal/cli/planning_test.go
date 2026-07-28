package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestPlanningReadCommandsReturnJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" && r.URL.Query().Get("maxResults") == "1" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		switch r.URL.Path {
		case "/rest/agile/1.0/board":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":2,"isLast":true,"values":[
				{"id":7,"name":"Scrum","type":"scrum"},{"id":8,"name":"Kanban","type":"kanban"}
			]}`))
		case "/rest/agile/1.0/board/7":
			_, _ = w.Write([]byte(`{"id":7,"name":"Scrum","type":"scrum"}`))
		case "/rest/software/1.0/board/7/issue", "/rest/software/1.0/board/7/backlog":
			_, _ = w.Write([]byte(`{"maxResults":50,"isLast":true,"issues":[{"id":"1","key":"ENG-1","fields":{"summary":"One"}}]}`))
		case "/rest/agile/1.0/board/7/sprint":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"isLast":true,"values":[{"id":9,"state":"active","name":"Sprint"}]}`))
		case "/rest/agile/1.0/sprint/9":
			_, _ = w.Write([]byte(`{"id":9,"state":"active","name":"Sprint"}`))
		case "/rest/software/1.0/sprint/9/issue":
			_, _ = w.Write([]byte(`{"maxResults":50,"isLast":true,"issues":[{"id":"1","key":"ENG-1","fields":{"summary":"One"}}]}`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()
	configPath := writeCommentsConfig(t, server.URL, "cloud")

	commands := [][]string{
		{"boards", "list", "--json"},
		{"boards", "get", "7", "--json"},
		{"boards", "issues", "7", "--json"},
		{"boards", "backlog", "7", "--json"},
		{"sprints", "list", "7", "--state", "active", "--json"},
		{"sprints", "get", "9", "--json"},
		{"sprints", "issues", "9", "--json"},
	}
	for _, command := range commands {
		t.Run(strings.Join(command[:2], "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", configPath}, command...)
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatalf("error = %v, stderr = %s", err, stderr.String())
			}
			var envelope struct {
				OK   bool            `json:"ok"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil ||
				!envelope.OK || len(envelope.Data) == 0 {
				t.Fatalf("stdout = %s, err = %v", stdout.String(), err)
			}
		})
	}
}

func TestCloudPlanningAllUsesTokensAndHardLimit(t *testing.T) {
	var issueRequests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		if r.URL.Path != "/rest/software/1.0/board/7/backlog" {
			http.NotFound(w, r)
			return
		}
		issueRequests = append(issueRequests, r.URL.RawQuery)
		switch r.URL.Query().Get("nextPageToken") {
		case "":
			if r.URL.Query().Get("maxResults") != "2" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"maxResults":2,"isLast":false,"nextPageToken":"t1","issues":[
				{"id":"1","key":"ENG-1","fields":{}},{"id":"2","key":"ENG-2","fields":{}}
			]}`))
		case "t1":
			if r.URL.Query().Get("maxResults") != "1" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"maxResults":1,"isLast":false,"nextPageToken":"t2","issues":[
				{"id":"3","key":"ENG-3","fields":{}}
			]}`))
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("nextPageToken"))
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "cloud"),
		"boards", "backlog", "7", "--max", "2", "--all", "--limit", "3", "--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("error = %v, stderr = %s", err, stderr.String())
	}
	var envelope struct {
		Data struct {
			Issues []any `json:"issues"`
			Page   struct {
				Returned int    `json:"returned"`
				Next     string `json:"next"`
				HasMore  bool   `json:"hasMore"`
				Pages    int    `json:"pages"`
			} `json:"page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Issues) != 3 || envelope.Data.Page.Returned != 3 ||
		envelope.Data.Page.Next != "t2" || !envelope.Data.Page.HasMore ||
		envelope.Data.Page.Pages != 2 || len(issueRequests) != 2 {
		t.Fatalf("stdout = %s, requests = %#v", stdout.String(), issueRequests)
	}
}

func TestBoardAndSprintListsRespectHardLimits(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		path    string
		values  string
	}{
		{
			name:    "boards",
			command: []string{"boards", "list"},
			path:    "/rest/agile/1.0/board",
			values:  `{"id":%d,"name":"Board %d","type":"scrum"}`,
		},
		{
			name:    "sprints",
			command: []string{"sprints", "list", "7"},
			path:    "/rest/agile/1.0/board/7/sprint",
			values:  `{"id":%d,"name":"Sprint %d","state":"future"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pageRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/agile/1.0/board" &&
					r.URL.Query().Get("maxResults") == "1" &&
					!r.URL.Query().Has("startAt") {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				if r.URL.Path != test.path {
					http.NotFound(w, r)
					return
				}
				pageRequests++
				start, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
				maxResults, _ := strconv.Atoi(r.URL.Query().Get("maxResults"))
				if pageRequests == 1 && (start != 0 || maxResults != 2) {
					t.Fatalf("first query = %s", r.URL.RawQuery)
				}
				if pageRequests == 2 && (start != 2 || maxResults != 1) {
					t.Fatalf("second query = %s", r.URL.RawQuery)
				}
				values := make([]string, 0, maxResults)
				for i := 0; i < maxResults; i++ {
					id := start + i + 1
					values = append(values, fmt.Sprintf(test.values, id, id))
				}
				_, _ = fmt.Fprintf(w, `{"startAt":%d,"maxResults":%d,"total":4,"isLast":false,"values":[%s]}`,
					start, maxResults, strings.Join(values, ","))
			}))
			defer server.Close()

			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", writeCommentsConfig(t, server.URL, "data_center")}, test.command...)
			args = append(args, "--max", "2", "--all", "--limit", "3", "--json")
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatalf("error = %v, stderr = %s", err, stderr.String())
			}
			var envelope struct {
				Data struct {
					Page struct {
						Returned int  `json:"returned"`
						Next     int  `json:"next"`
						HasMore  bool `json:"hasMore"`
					} `json:"page"`
				} `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if pageRequests != 2 || envelope.Data.Page.Returned != 3 ||
				envelope.Data.Page.Next != 3 || !envelope.Data.Page.HasMore {
				t.Fatalf("requests = %d, stdout = %s", pageRequests, stdout.String())
			}
		})
	}
}

func TestPlanningWritesUseExactCLIOrdering(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		method   string
		path     string
		query    string
		wantBody map[string]any
		response string
	}{
		{
			name:     "sprint move",
			args:     []string{"sprints", "move", "9", "--issue", "ENG-2,ENG-1", "--json"},
			method:   http.MethodPost,
			path:     "/rest/agile/1.0/sprint/9/issue",
			wantBody: map[string]any{"issues": []any{"ENG-2", "ENG-1"}},
		},
		{
			name:     "backlog move",
			args:     []string{"backlog", "move", "--board", "7", "--issue", "ENG-2", "--issue", "ENG-1", "--json"},
			method:   http.MethodPost,
			path:     "/rest/agile/1.0/backlog/7/issue",
			wantBody: map[string]any{"issues": []any{"ENG-2", "ENG-1"}},
		},
		{
			name:   "rank",
			args:   []string{"rank", "--issue", "ENG-2", "--issue", "ENG-1", "--after", "ENG-3", "--rank-field", "10521", "--json"},
			method: http.MethodPut,
			path:   "/rest/agile/1.0/issue/rank",
			wantBody: map[string]any{
				"issues": []any{"ENG-2", "ENG-1"}, "rankAfterIssue": "ENG-3",
				"rankCustomFieldId": json.Number("10521"),
			},
		},
		{
			name:     "estimate",
			args:     []string{"estimate", "ENG-1", "--board", "7", "--value", "8.0", "--json"},
			method:   http.MethodPut,
			path:     "/rest/agile/1.0/issue/ENG-1/estimation",
			query:    "boardId=7",
			wantBody: map[string]any{"value": "8.0"},
			response: `{"fieldId":"customfield_10016","value":"8.0"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutations := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/agile/1.0/board" {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				mutations++
				if r.Method != test.method || r.URL.Path != test.path ||
					(test.query != "" && r.URL.RawQuery != test.query) {
					t.Fatalf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
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
				if test.response != "" {
					_, _ = w.Write([]byte(test.response))
				} else {
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			deployment := "data_center"
			if test.name == "backlog move" {
				deployment = "cloud"
			}
			args := append([]string{"--config", writeCommentsConfig(t, server.URL, deployment)}, test.args...)
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatalf("error = %v, stderr = %s", err, stderr.String())
			}
			if mutations != 1 || !strings.Contains(stdout.String(), `"ok": true`) {
				t.Fatalf("mutations = %d, stdout = %s", mutations, stdout.String())
			}
		})
	}
}

func TestPlanningMultiStatusIsStructuredFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte(`{"entries":[{"issueId":"ENG-2","errors":["rank failed"]}]}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "cloud"),
		"rank", "--issue", "ENG-2,ENG-1", "--before", "ENG-3", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || stderr.Len() != 0 ||
		!strings.Contains(stdout.String(), `"ok": false`) ||
		!strings.Contains(stdout.String(), `"kind": "partial_failure"`) ||
		!strings.Contains(stdout.String(), `"rank failed"`) {
		t.Fatalf("error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
	}
}

func TestEmptyEstimateResponseStillReportsAcceptedWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"estimate", "ENG-1", "--board", "7", "--value", "8", "--json",
	}, &stdout, &stderr)
	if err != nil || !strings.Contains(stdout.String(), `"accepted": true`) {
		t.Fatalf("error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
	}
}

func TestPlanningCapabilityFailuresKeepJSONContract(t *testing.T) {
	tests := []struct {
		status int
		kind   string
	}{
		{status: http.StatusNotFound, kind: "unsupported"},
		{status: http.StatusForbidden, kind: "auth"},
	}
	for _, test := range tests {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"errorMessages":["software unavailable"]}`))
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			err := Run([]string{
				"--config", writeCommentsConfig(t, server.URL, "data_center"),
				"boards", "list", "--json",
			}, &stdout, &stderr)
			if err == nil || !IsReported(err) || stdout.Len() != 0 ||
				!strings.Contains(stderr.String(), fmt.Sprintf(`"kind": %q`, test.kind)) ||
				requests != 1 {
				t.Fatalf("error = %v, stdout = %s, stderr = %s, requests = %d", err, stdout.String(), stderr.String(), requests)
			}
		})
	}
}

func TestPlanningWriteLimitsAndIDsFailBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()
	configPath := writeCommentsConfig(t, server.URL, "data_center")

	var tooMany []string
	for i := 0; i < 51; i++ {
		tooMany = append(tooMany, fmt.Sprintf("ENG-%d", i+1))
	}
	tests := [][]string{
		{"--config", configPath, "sprints", "move", "0", "--issue", "ENG-1"},
		{"--config", configPath, "rank", "--issue", strings.Join(tooMany, ","), "--after", "ENG-99"},
		{"--config", configPath, "estimate", "ENG-1", "--board", "not-an-id", "--value", "8"},
	}
	for _, args := range tests {
		if err := (App{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Run(args); err == nil {
			t.Fatalf("args = %#v returned nil", args)
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestPlanningWritesAreNotRetried(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		mutations++
		http.Error(w, `{"errorMessages":["temporary"]}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"rank", "--issue", "ENG-1", "--after", "ENG-2", "--json",
	}, &stdout, &stderr)
	if err == nil || mutations != 1 {
		t.Fatalf("error = %v, mutations = %d", err, mutations)
	}
}
