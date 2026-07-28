package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseGlobalFlags(t *testing.T) {
	var path string
	args, err := parseGlobalFlags([]string{"--config", "local.toml", "search", "--profile", "mine"}, &path)
	if err != nil {
		t.Fatal(err)
	}
	if path != "local.toml" {
		t.Fatalf("path = %q", path)
	}
	if len(args) != 3 || args[0] != "search" {
		t.Fatalf("args = %#v", args)
	}
}

func TestRunServerInfoJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/serverInfo" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"baseUrl":"https://jira.example.com","version":"1001.0.0","deploymentType":"Cloud"}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configBody := "[jira]\nbase_url = \"" + server.URL + "\"\ntoken = \"secret\"\ndeployment = \"auto\"\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--config", configPath, "server-info", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"deployment": "cloud"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"deploymentSource": "detected"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunSearchProfileUsesPostAndProfileDefaults(t *testing.T) {
	var requestBody struct {
		JQL        string   `json:"jql"`
		Fields     []string `json:"fields"`
		MaxResults int      `json:"maxResults"`
		StartAt    int      `json:"startAt"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/2/search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{
			"startAt":0,
			"maxResults":7,
			"total":1,
			"issues":[{"id":"1","key":"ENG-1","fields":{"summary":"Test"}}]
		}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configBody := `[jira]
base_url = "` + server.URL + `"
token = "secret"
deployment = "data_center"

[profiles.mine]
jql = "project = ENG ORDER BY updated DESC"
fields = ["summary", "status"]
max_results = 7
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--config", configPath, "search", "--profile", "mine", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if requestBody.JQL != "project = ENG ORDER BY updated DESC" ||
		requestBody.MaxResults != 7 ||
		!reflect.DeepEqual(requestBody.Fields, []string{"summary", "status"}) {
		t.Fatalf("request body = %#v", requestBody)
	}
	if !strings.Contains(stdout.String(), `"page"`) || !strings.Contains(stdout.String(), `"ENG-1"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunSearchLimitRequiresAll(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run([]string{"search", "--jql", "project = ENG", "--limit", "5"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--limit requires --all") {
		t.Fatalf("error = %v", err)
	}
}

func TestParsePositiveIDs(t *testing.T) {
	ids, err := parsePositiveIDs([]string{"10001", " 10002 "})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int64{10001, 10002}) {
		t.Fatalf("ids = %#v", ids)
	}
	if _, err := parsePositiveIDs([]string{"ENG-1"}); err == nil {
		t.Fatal("expected non-numeric ID error")
	}
}
