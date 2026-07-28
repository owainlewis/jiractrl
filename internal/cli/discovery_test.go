package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryCommandsReturnJSONEnvelopes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/project":
			_, _ = w.Write([]byte(`[{"id":"1","key":"ENG","name":"Engineering"}]`))
		case "/rest/api/2/project/ENG":
			_, _ = w.Write([]byte(`{"id":"1","key":"ENG","name":"Engineering","issueTypes":[{"id":"10","name":"Task"}]}`))
		case "/rest/api/2/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"1","key":"ENG","name":"Engineering","issuetypes":[{"id":"10","name":"Task","fields":{"summary":{"name":"Summary","required":true,"allowedValues":[]}}}]}]}`))
		case "/rest/api/2/issue/ENG-1/editmeta":
			_, _ = w.Write([]byte(`{"fields":{"summary":{"name":"Summary","required":true,"allowedValues":[]}}}`))
		case "/rest/api/2/user/assignable/search":
			_, _ = w.Write([]byte(`[{"name":"john","key":"jdoe","displayName":"John Doe","active":true}]`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := "[jira]\nbase_url = \"" + server.URL + "\"\ntoken = \"secret\"\ndeployment = \"data_center\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"projects", "list", "--json"},
		{"projects", "get", "ENG", "--json"},
		{"projects", "issue-types", "ENG", "--json"},
		{"meta", "create", "--project", "ENG", "--type", "Task", "--json"},
		{"meta", "edit", "ENG-1", "--json"},
		{"users", "assignable", "--project", "ENG", "--query", "john", "--json"},
	}
	for _, command := range commands {
		t.Run(command[0]+"_"+command[1], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", configPath}, command...)
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatalf("%v: stderr=%s", err, stderr.String())
			}
			var envelope struct {
				OK   bool            `json:"ok"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || !envelope.OK || len(envelope.Data) == 0 {
				t.Fatalf("stdout = %s, err = %v", stdout.String(), err)
			}
		})
	}
}

func TestAmbiguousIssueTypeJSONErrorReturnsCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":2,"isLast":true,"issueTypes":[{"id":"10","name":"Task"},{"id":"11","name":"Task"}]}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := "[jira]\nbase_url = \"" + server.URL + "\"\ntoken = \"secret\"\nemail = \"agent@example.com\"\ndeployment = \"cloud\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath,
		"meta", "create", "--project", "ENG", "--type", "Task", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) {
		t.Fatalf("error = %v", err)
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Kind       string `json:"kind"`
			Candidates []struct {
				ID string `json:"id"`
			} `json:"candidates"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error.Kind != "ambiguous" || len(envelope.Error.Candidates) != 2 {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
