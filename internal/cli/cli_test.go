package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
