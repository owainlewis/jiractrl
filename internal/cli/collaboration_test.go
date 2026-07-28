package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorklogValidationFailsBeforeMutation(t *testing.T) {
	for _, args := range [][]string{
		{"worklogs", "add", "ENG-1", "--time-spent", "zero"},
		{"worklogs", "add", "ENG-1", "--time-spent", "1h", "--adjust", "new"},
		{"worklogs", "add", "ENG-1", "--time-spent", "1h", "--adjust", "leave", "--new-estimate", "1d"},
		{"worklogs", "update", "ENG-1", "10", "--adjust", "manual", "--reduce-by", "1h"},
		{"worklogs", "add", "ENG-1", "--time-spent", "1h", "--started", "not-a-time"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := Run(args, &stdout, &stderr); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCloudWorklogAddExactPayload(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue/ENG-1/worklog" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("adjustEstimate") != "new" || r.URL.Query().Get("newEstimate") != "2d" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"10","timeSpent":"1h"}`))
	}))
	defer server.Close()
	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "worklogs", "add", "ENG-1",
		"--time-spent", "1h", "--started", "2026-07-28T12:30:00Z",
		"--comment", "**done**", "--visibility-type", "role", "--visibility-value", "Developers",
		"--adjust", "new", "--new-estimate", "2d", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if body["timeSpent"] != "1h" || body["started"] != "2026-07-28T12:30:00.000+0000" {
		t.Fatalf("body = %#v", body)
	}
	comment := body["comment"].(map[string]any)
	if comment["type"] != "doc" || !strings.Contains(stdout.String(), `"id": "10"`) {
		t.Fatalf("body = %#v, stdout = %s", body, stdout.String())
	}
}

func TestDataCenterWorklogUpdateUsesStringComment(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/2/issue/ENG-1/worklog/10" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"10","comment":"plain **text**"}`))
	}))
	defer server.Close()
	configPath := writeCommentsConfig(t, server.URL, "data_center")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "worklogs", "update", "ENG-1", "10", "--comment", "plain **text**",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if body["comment"] != "plain **text**" {
		t.Fatalf("body = %#v", body)
	}
}

func TestWorklogsListAllRespectsLimit(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query().Get("startAt")+":"+r.URL.Query().Get("maxResults"))
		if r.URL.Query().Get("startAt") == "0" {
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":2,"total":4,"worklogs":[{"id":"1"},{"id":"2"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"startAt":2,"maxResults":1,"total":4,"worklogs":[{"id":"3"}]}`))
		}
	}))
	defer server.Close()
	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "worklogs", "list", "ENG-1", "--max", "2", "--all", "--limit", "3", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if strings.Join(requests, ",") != "0:2,2:1" ||
		!strings.Contains(stdout.String(), `"returned": 3`) ||
		!strings.Contains(stdout.String(), `"hasMore": true`) {
		t.Fatalf("requests = %#v, stdout = %s", requests, stdout.String())
	}
}

func TestWatcherDeploymentIdentityAndSelfBodies(t *testing.T) {
	tests := []struct {
		name       string
		deployment string
		args       []string
		method     string
		path       string
		query      string
		body       string
		myself     string
	}{
		{name: "cloud add", deployment: "cloud", args: []string{"add", "ENG-1", "--account-id", "a1"}, method: "POST", path: "/rest/api/3/issue/ENG-1/watchers", body: `"a1"`},
		{name: "dc remove", deployment: "data_center", args: []string{"remove", "ENG-1", "--user", "john"}, method: "DELETE", path: "/rest/api/2/issue/ENG-1/watchers", query: "username=john"},
		{name: "self add", deployment: "cloud", args: []string{"add", "ENG-1", "--self"}, method: "POST", path: "/rest/api/3/issue/ENG-1/watchers", body: ""},
		{name: "cloud self remove", deployment: "cloud", args: []string{"remove", "ENG-1", "--self"}, method: "DELETE", path: "/rest/api/3/issue/ENG-1/watchers", query: "accountId=me-cloud", myself: `{"accountId":"me-cloud"}`},
		{name: "dc self add", deployment: "data_center", args: []string{"add", "ENG-1", "--self"}, method: "POST", path: "/rest/api/2/issue/ENG-1/watchers", body: `"me-server"`, myself: `{"name":"me-server"}`},
		{name: "dc self remove", deployment: "data_center", args: []string{"remove", "ENG-1", "--self"}, method: "DELETE", path: "/rest/api/2/issue/ENG-1/watchers", query: "username=me-server", myself: `{"name":"me-server"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/myself") {
					_, _ = w.Write([]byte(test.myself))
					return
				}
				if r.Method != test.method || r.URL.Path != test.path || r.URL.RawQuery != test.query {
					t.Fatalf("request = %s %s", r.Method, r.URL.RequestURI())
				}
				var raw any
				if r.Body != nil {
					data := new(bytes.Buffer)
					_, _ = data.ReadFrom(r.Body)
					raw = strings.TrimSpace(data.String())
				}
				if raw != test.body {
					t.Fatalf("body = %#v, want %q", raw, test.body)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			configPath := writeCommentsConfig(t, server.URL, test.deployment)
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", configPath, "watchers"}, test.args...)
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatalf("%v: %s", err, stderr.String())
			}
		})
	}
}

func TestWatcherPrivacyFailureIsStructured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["View voters and watchers permission required"]}`, http.StatusForbidden)
	}))
	defer server.Close()
	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "watchers", "list", "ENG-1", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) ||
		!strings.Contains(stderr.String(), `"kind": "auth"`) ||
		!strings.Contains(stderr.String(), `"status": 403`) {
		t.Fatalf("error = %v, stderr = %s", err, stderr.String())
	}
}
