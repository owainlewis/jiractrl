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

func TestCloudCommentAddConvertsMarkdownAndPreservesVisibility(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue/ENG-1/comment" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"12","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"created"}]}]}}`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "comments", "add", "ENG-1",
		"--body", "# Heading\n\nUse **bold** and [Jira](https://jira.example.com).",
		"--visibility-type", "role", "--visibility-value", "Developers", "--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}

	body := requestBody["body"].(map[string]any)
	if body["type"] != "doc" || body["version"].(float64) != 1 {
		t.Fatalf("body = %#v", body)
	}
	encoded, _ := json.Marshal(body)
	for _, marker := range []string{`"type":"heading"`, `"type":"strong"`, `"type":"link"`} {
		if !strings.Contains(string(encoded), marker) {
			t.Fatalf("ADF missing %s: %s", marker, encoded)
		}
	}
	visibility := requestBody["visibility"].(map[string]any)
	if visibility["type"] != "role" || visibility["value"] != "Developers" {
		t.Fatalf("visibility = %#v", visibility)
	}
	if !strings.Contains(stdout.String(), `"id": "12"`) || !strings.Contains(stdout.String(), `"type": "doc"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestCommentAliasUsesDataCenterStringBody(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/2/issue/ENG-1/comment" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"12","body":"plain **text**"}`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "data_center")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "comment", "ENG-1", "--body", "plain **text**",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if requestBody["body"] != "plain **text**" {
		t.Fatalf("body = %#v", requestBody["body"])
	}
}

func TestCloudCommentUpdateAcceptsRawADFInput(t *testing.T) {
	rawADF := `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"exact","marks":[{"type":"strong"}]}]}]}`
	inputPath := filepath.Join(t.TempDir(), "comment.json")
	if err := os.WriteFile(inputPath, []byte(`{"body":`+rawADF+`,"visibility":{"type":"group","value":"jira-users","identifier":"group-100"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/ENG-1/comment/12" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"12","body":` + rawADF + `}`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "comments", "update", "ENG-1", "12", "--input", inputPath, "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	body, _ := json.Marshal(requestBody["body"])
	var want any
	if err := json.Unmarshal([]byte(rawADF), &want); err != nil {
		t.Fatal(err)
	}
	wantBody, _ := json.Marshal(want)
	if string(body) != string(wantBody) {
		t.Fatalf("body = %s, want %s", body, wantBody)
	}
	visibility := requestBody["visibility"].(map[string]any)
	if visibility["identifier"] != "group-100" {
		t.Fatalf("visibility = %#v", visibility)
	}
}

func TestCommentsListAllIsBoundedAndKeepsRichJSON(t *testing.T) {
	var starts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, r.URL.Query().Get("startAt")+":"+r.URL.Query().Get("maxResults"))
		switch r.URL.Query().Get("startAt") {
		case "0":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":2,"total":4,"comments":[
				{"id":"1","author":{"displayName":"One"},"body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}},
				{"id":"2","author":{"displayName":"Two"},"body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}}
			]}`))
		case "2":
			_, _ = w.Write([]byte(`{"startAt":2,"maxResults":1,"total":4,"comments":[
				{"id":"3","author":{"displayName":"Three"},"body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"third"}]}]}}
			]}`))
		default:
			t.Fatalf("unexpected startAt %q", r.URL.Query().Get("startAt"))
		}
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "comments", "list", "ENG-1", "--max", "2", "--all", "--limit", "3", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if strings.Join(starts, ",") != "0:2,2:1" {
		t.Fatalf("requests = %#v", starts)
	}
	if !strings.Contains(stdout.String(), `"returned": 3`) ||
		!strings.Contains(stdout.String(), `"hasMore": true`) ||
		!strings.Contains(stdout.String(), `"type": "doc"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestCommentsRemoveReturnsExplicitReceipt(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodDelete || r.URL.Path != "/rest/api/3/issue/ENG-1/comment/12" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "comments", "remove", "ENG-1", "12", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if requests != 1 || !strings.Contains(stdout.String(), `"removed": true`) {
		t.Fatalf("requests = %d, stdout = %s", requests, stdout.String())
	}
}

func TestCommentsListAllCompletesAndRendersCloudADFAsText(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Query().Get("startAt") {
		case "0":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":1,"total":2,"comments":[
				{"id":"1","author":{"displayName":"One"},"body":{"type":"doc","version":1,"content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"First"}]}]}}
			]}`))
		case "1":
			_, _ = w.Write([]byte(`{"startAt":1,"maxResults":1,"total":2,"comments":[
				{"id":"2","author":{"displayName":"Two"},"body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Second"}]}]}}
			]}`))
		default:
			t.Fatalf("unexpected startAt %q", r.URL.Query().Get("startAt"))
		}
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "comments", "list", "ENG-1", "--max", "1", "--all",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if requests != 2 || !strings.Contains(stdout.String(), "First") ||
		!strings.Contains(stdout.String(), "Second") ||
		strings.Contains(stdout.String(), "More comments available") {
		t.Fatalf("requests = %d, stdout = %s", requests, stdout.String())
	}
}

func TestCommentsListRendersDataCenterStringAsText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":1,"comments":[{"id":"1","author":{"displayName":"One"},"body":"Data Center body"}]}`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "data_center")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "comments", "list", "ENG-1",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Data Center body") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestCloudStructuredStringCommentIsRejectedBeforeWrite(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "comment.json")
	if err := os.WriteFile(inputPath, []byte(`{"body":"not ADF"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "comments", "add", "ENG-1", "--input", inputPath,
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "ADF object") {
		t.Fatalf("error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func writeCommentsConfig(t *testing.T, baseURL, deployment string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[jira]\nbase_url = \"" + baseURL + "\"\ntoken = \"secret\"\ndeployment = \"" + deployment + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
