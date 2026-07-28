package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssignCloudAccountIDExactBody(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/user/assignable/search":
			if r.URL.Query().Get("accountId") != "a1" || r.URL.Query().Get("issueKey") != "ENG-1" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"accountId":"a1","displayName":"Alex","active":true}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/3/issue/ENG-1/assignee":
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "assign", "ENG-1", "--account-id", "a1", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if requestBody["accountId"] != "a1" || len(requestBody) != 1 ||
		!strings.Contains(stdout.String(), `"accountId": "a1"`) {
		t.Fatalf("body = %#v, stdout = %s", requestBody, stdout.String())
	}
}

func TestInvalidCloudAccountIDFailsBeforeMutation(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mutations++
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "assign", "ENG-1", "--account-id", "missing",
	}, &stdout, &stderr)
	if err == nil || mutations != 0 || !strings.Contains(err.Error(), "no exact assignable user") {
		t.Fatalf("error = %v, mutations = %d", err, mutations)
	}
}

func TestDataCenterRejectsCloudAccountIDBeforeMutation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "data_center")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "assign", "ENG-1", "--account-id", "a1", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || requests != 0 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
	if !strings.Contains(stderr.String(), `"kind": "validation"`) ||
		!strings.Contains(stderr.String(), `"account-id"`) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestAssignUserResolvesDeploymentIdentity(t *testing.T) {
	tests := []struct {
		name       string
		deployment string
		searchPath string
		searchBody string
		assignPath string
		field      string
		value      string
	}{
		{
			name:       "cloud display name",
			deployment: "cloud",
			searchPath: "/rest/api/3/user/assignable/search",
			searchBody: `[{"accountId":"a1","displayName":"Alex Smith","active":true}]`,
			assignPath: "/rest/api/3/issue/ENG-1/assignee",
			field:      "accountId",
			value:      "a1",
		},
		{
			name:       "data center username",
			deployment: "data_center",
			searchPath: "/rest/api/2/user/assignable/search",
			searchBody: `[{"name":"alex","key":"asmith","displayName":"Alex Smith","active":true}]`,
			assignPath: "/rest/api/2/issue/ENG-1/assignee",
			field:      "name",
			value:      "alex",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requestBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == test.searchPath:
					_, _ = w.Write([]byte(test.searchBody))
				case r.Method == http.MethodPut && r.URL.Path == test.assignPath:
					if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
						t.Fatal(err)
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					http.Error(w, "unexpected", http.StatusNotFound)
				}
			}))
			defer server.Close()

			configPath := writeCommentsConfig(t, server.URL, test.deployment)
			var stdout, stderr bytes.Buffer
			query := "Alex Smith"
			if test.deployment == "data_center" {
				query = "alex"
			}
			if err := Run([]string{
				"--config", configPath, "assign", "ENG-1", "--user", query,
			}, &stdout, &stderr); err != nil {
				t.Fatalf("%v: %s", err, stderr.String())
			}
			if requestBody[test.field] != test.value || len(requestBody) != 1 {
				t.Fatalf("body = %#v", requestBody)
			}
		})
	}
}

func TestAssignUserAmbiguousJSONDoesNotMutate(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mutations++
		}
		_, _ = w.Write([]byte(`[
			{"accountId":"a1","displayName":"Alex Smith","active":true},
			{"accountId":"a2","displayName":"Alex Smith","active":true}
		]`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "assign", "ENG-1", "--user", "Alex Smith", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || mutations != 0 {
		t.Fatalf("error = %v, mutations = %d", err, mutations)
	}
	if !strings.Contains(stderr.String(), `"kind": "ambiguous"`) ||
		!strings.Contains(stderr.String(), `"accountId": "a1"`) ||
		!strings.Contains(stderr.String(), `"accountId": "a2"`) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestAssignInvalidUserDoesNotMutate(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mutations++
		}
		_, _ = w.Write([]byte(`[{"accountId":"a1","displayName":"Alexandra","active":true}]`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "assign", "ENG-1", "--user", "Alex",
	}, &stdout, &stderr)
	if err == nil || mutations != 0 || !strings.Contains(err.Error(), "no exact assignable user") {
		t.Fatalf("error = %v, mutations = %d", err, mutations)
	}
}

func TestAssignUnassignUsesDeploymentNullField(t *testing.T) {
	for _, test := range []struct {
		deployment string
		path       string
		field      string
	}{
		{deployment: "cloud", path: "/rest/api/3/issue/ENG-1/assignee", field: "accountId"},
		{deployment: "data_center", path: "/rest/api/2/issue/ENG-1/assignee", field: "name"},
	} {
		t.Run(test.deployment, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Fatalf("path = %s", r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			configPath := writeCommentsConfig(t, server.URL, test.deployment)
			var stdout, stderr bytes.Buffer
			if err := Run([]string{
				"--config", configPath, "assign", "ENG-1", "--unassign",
			}, &stdout, &stderr); err != nil {
				t.Fatalf("%v: %s", err, stderr.String())
			}
			value, exists := body[test.field]
			if !exists || value != nil || len(body) != 1 {
				t.Fatalf("body = %#v", body)
			}
		})
	}
}

func TestCreateSubtaskValidatesTypeAndSendsParent(t *testing.T) {
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta/ENG/issuetypes":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":1,"isLast":true,"issueTypes":[{"id":"10001","name":"Sub-task","subtask":true}]}`))
		case "/rest/api/2/issue":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"2","key":"ENG-2"}`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "create",
		"--project", "ENG", "--type", "Sub-task", "--summary", "Child", "--parent", "ENG-1", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	fields := createBody["fields"].(map[string]any)
	if fields["parent"].(map[string]any)["key"] != "ENG-1" ||
		fields["issuetype"].(map[string]any)["id"] != "10001" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestStructuredSubtaskCreatePreservesExactParentShape(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "subtask.json")
	input := `{"fields":{"project":{"key":"ENG"},"issuetype":{"id":"10001"},"summary":"Child","parent":{"id":"10000"}}}`
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"2","key":"ENG-2"}`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "data_center")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "create", "--input", inputPath, "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	fields := createBody["fields"].(map[string]any)
	if fields["parent"].(map[string]any)["id"] != "10000" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestCreateParentRejectsNonSubtaskBeforeMutation(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta/ENG/issuetypes":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":1,"isLast":true,"issueTypes":[{"id":"10001","name":"Task","subtask":false}]}`))
		case "/rest/api/2/issue":
			mutations++
			http.Error(w, "unexpected", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "create",
		"--project", "ENG", "--type", "Task", "--summary", "Child", "--parent", "ENG-1",
	}, &stdout, &stderr)
	if err == nil || mutations != 0 || !strings.Contains(err.Error(), "subtask issue type") {
		t.Fatalf("error = %v, mutations = %d", err, mutations)
	}
}

func TestCloudParentUpdateExactBody(t *testing.T) {
	var updateBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/2/issue/ENG-2" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "update", "ENG-2", "--parent", "ENG-1", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	fields := updateBody["fields"].(map[string]any)
	if fields["parent"].(map[string]any)["key"] != "ENG-1" {
		t.Fatalf("body = %#v", updateBody)
	}
}

func TestDataCenterParentUpdateFailsBeforeMutation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "data_center")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "update", "ENG-2", "--parent", "ENG-1", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || requests != 0 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
	if !strings.Contains(stderr.String(), `"kind": "unsupported"`) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
