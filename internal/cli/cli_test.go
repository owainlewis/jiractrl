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
		if r.URL.Path == "/rest/agile/1.0/board" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		if r.URL.Path == "/rest/servicedeskapi/servicedesk" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
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
	if !strings.Contains(stdout.String(), `"service_management": "available"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	var envelope struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || !envelope.OK {
		t.Fatalf("envelope = %#v, err = %v", envelope, err)
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

func TestJSONSuccessEnvelopeCoversPreviouslyTextOnlyCommands(t *testing.T) {
	var mutations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"Agent"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/2/issue/ENG-1":
			mutations = append(mutations, "update")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1/transitions":
			_, _ = w.Write([]byte(`{"transitions":[{"id":"31","name":"Done"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ENG-1/transitions":
			mutations = append(mutations, "transition")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configBody := `[jira]
base_url = "` + server.URL + `"
token = "secret"
deployment = "data_center"

[profiles.mine]
jql = "project = ENG"
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	commands := [][]string{
		{"auth", "check", "--json"},
		{"update", "ENG-1", "--summary", "Updated", "--json"},
		{"transition", "ENG-1", "--to", "Done", "--json"},
		{"profiles", "list", "--json"},
		{"profiles", "show", "mine", "--json"},
	}
	for _, command := range commands {
		t.Run(strings.Join(command, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", configPath}, command...)
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				OK   bool            `json:"ok"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout = %s: %v", stdout.String(), err)
			}
			if !envelope.OK || len(envelope.Data) == 0 {
				t.Fatalf("envelope = %#v", envelope)
			}
		})
	}
	if !reflect.DeepEqual(mutations, []string{"update", "transition"}) {
		t.Fatalf("mutations = %#v", mutations)
	}
}

func TestJSONSuccessEnvelopeCoversReadAndWriteCommandFamilies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1":
			_, _ = w.Write([]byte(`{"id":"1","key":"ENG-1","fields":{"summary":"Test"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"2","key":"ENG-2"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ENG-1/comment":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"body":"Context"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1/transitions":
			_, _ = w.Write([]byte(`{"transitions":[{"id":"31","name":"Done"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/field":
			_, _ = w.Write([]byte(`[{"id":"summary","name":"Summary"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/search":
			_, _ = w.Write([]byte(`{
				"startAt":0,
				"maxResults":10,
				"total":1,
				"issues":[{"id":"1","key":"ENG-1","fields":{"summary":"Test"}}]
			}`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configBody := "[jira]\nbase_url = \"" + server.URL + "\"\ntoken = \"secret\"\ndeployment = \"data_center\"\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	commands := [][]string{
		{"get", "ENG-1", "--json"},
		{"create", "--project", "ENG", "--summary", "Created", "--json"},
		{"comment", "ENG-1", "--body", "Context", "--json"},
		{"transitions", "ENG-1", "--json"},
		{"fields", "--json"},
		{"issue-fields", "ENG-1", "--json"},
		{"triage", "--jql", "project = ENG", "--json"},
	}
	for _, command := range commands {
		t.Run(command[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", configPath}, command...)
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatalf("%v: stderr=%s", err, stderr.String())
			}
			var envelope struct {
				OK   bool            `json:"ok"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout = %s: %v", stdout.String(), err)
			}
			if !envelope.OK || len(envelope.Data) == 0 {
				t.Fatalf("envelope = %#v", envelope)
			}
		})
	}
}

func TestJSONErrorEnvelopeRedactsTokenAndIncludesRetryMetadata(t *testing.T) {
	const token = "top-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		w.Header().Set("Retry-After", "7")
		w.Header().Set("RateLimit-Reason", "jira-quota-based")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{
			"errorMessages":["authorization ` + authorization + ` was rejected"],
			"errors":{"jql":"contains top-secret-token"}
		}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configBody := `[jira]
base_url = "` + server.URL + `"
token = "` + token + `"
deployment = "data_center"

[retry]
max_attempts = 1
base_delay_ms = 1
max_delay_ms = 10
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := Run([]string{"--config", configPath, "search", "--jql", "project = ENG", "--json"}, &stdout, &stderr)
	if err == nil || !IsReported(err) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(stderr.String(), token) ||
		strings.Contains(stdout.String(), token) ||
		strings.Contains(stderr.String(), "Bearer "+token) {
		t.Fatalf("secret leaked: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Kind   string `json:"kind"`
			Status int    `json:"status"`
			Retry  struct {
				Attempts  int    `json:"attempts"`
				Retryable bool   `json:"retryable"`
				Reason    string `json:"reason"`
			} `json:"retry"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error.Kind != "rate_limited" || envelope.Error.Status != 429 {
		t.Fatalf("envelope = %#v", envelope)
	}
	if envelope.Error.Retry.Attempts != 1 ||
		!envelope.Error.Retry.Retryable ||
		envelope.Error.Retry.Reason != "jira-quota-based" {
		t.Fatalf("retry = %#v", envelope.Error.Retry)
	}
	if ExitCode(err) != 6 {
		t.Fatalf("exit code = %d", ExitCode(err))
	}
}

func TestJSONFailureEnvelopeCoversEveryCommand(t *testing.T) {
	for _, name := range []string{"JIRACTRL_TOKEN", "JIRA_PAT", "JIRA_TOKEN"} {
		t.Setenv(name, "")
	}
	configPath := filepath.Join(t.TempDir(), "missing.toml")
	commands := [][]string{
		{"auth", "check", "--json"},
		{"server-info", "--json"},
		{"search", "--json"},
		{"get", "--json"},
		{"create", "--json"},
		{"update", "ENG-1", "--json"},
		{"comment", "ENG-1", "--json"},
		{"transitions", "--json"},
		{"transition", "ENG-1", "--json"},
		{"fields", "--json"},
		{"issue-fields", "--json"},
		{"profiles", "list", "--json"},
		{"triage", "--json"},
	}
	for _, command := range commands {
		t.Run(command[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", configPath}, command...)
			err := Run(args, &stdout, &stderr)
			if err == nil || !IsReported(err) {
				t.Fatalf("error = %v", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %s", stdout.String())
			}
			var envelope struct {
				OK    bool `json:"ok"`
				Error struct {
					Kind    string `json:"kind"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
				t.Fatalf("stderr = %s: %v", stderr.String(), err)
			}
			if envelope.OK || envelope.Error.Kind == "" || envelope.Error.Message == "" {
				t.Fatalf("envelope = %#v", envelope)
			}
		})
	}
}

func TestRawJSONPreservesJiraResponseBytes(t *testing.T) {
	const response = " \n{\"key\":\"ENG-1\",\"fields\":{\"summary\":\"Exact\"}}\t "
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configBody := "[jira]\nbase_url = \"" + server.URL + "\"\ntoken = \"secret\"\ndeployment = \"data_center\"\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--config", configPath, "get", "ENG-1", "--raw-json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != response {
		t.Fatalf("stdout = %q, want %q", stdout.String(), response)
	}
}

func TestSearchRawJSONPreservesJiraResponseBytes(t *testing.T) {
	const response = " \n{\"startAt\":0,\"total\":0,\"issues\":[]}\t "
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/2/search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configBody := "[jira]\nbase_url = \"" + server.URL + "\"\ntoken = \"secret\"\ndeployment = \"data_center\"\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath,
		"search", "--jql", "project = ENG", "--raw-json",
	}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != response {
		t.Fatalf("stdout = %q, want %q", stdout.String(), response)
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
