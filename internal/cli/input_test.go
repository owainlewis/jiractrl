package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStructuredMutationInputPreservesTypesAndExactBodies(t *testing.T) {
	requests := map[string]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		var body map[string]any
		if err := decoder.Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests[r.Method+" "+r.URL.Path] = body
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"2","key":"ENG-2"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := writeMutationConfig(t, dir, server.URL)
	createJSON := `{
		"fields": {
			"project": {"key":"ENG"},
			"issuetype": {"id":"10001"},
			"summary": "Typed",
			"customfield_number": 9007199254740993,
			"customfield_enabled": true,
			"customfield_cleared": null,
			"customfield_user": {"accountId":"abc"},
			"customfield_select": {"id":"20001"},
			"customfield_multi": [{"id":"20001"},{"id":"20002"}],
			"parent": {"key":"ENG-1"},
			"duedate": "2026-08-01"
		},
		"properties": [{"key":"agent","value":{"attempt":2}}]
	}`
	updateJSON := `{
		"fields": {"customfield_number": 3.5, "labels":["agent","typed"]},
		"update": {"components":[{"add":{"id":"30001"}}]}
	}`
	transitionJSON := `{
		"transition": {"id":"31"},
		"fields": {"resolution":{"id":"1"}},
		"properties": [{"key":"source","value":"agent"}]
	}`
	createInput := writeInputFile(t, dir, "create.json", createJSON)
	updateInput := writeInputFile(t, dir, "update.json", updateJSON)
	transitionInput := writeInputFile(t, dir, "transition.json", transitionJSON)

	commands := [][]string{
		{"create", "--input", createInput, "--json"},
		{"update", "ENG-2", "--input", updateInput, "--json"},
		{"transition", "ENG-2", "--input", transitionInput, "--json"},
	}
	for _, command := range commands {
		var stdout, stderr bytes.Buffer
		args := append([]string{"--config", configPath}, command...)
		if err := Run(args, &stdout, &stderr); err != nil {
			t.Fatalf("%s: %v, stderr=%s", command[0], err, stderr.String())
		}
	}

	create := requests["POST /rest/api/2/issue"]
	if want := decodeJSONMap(t, createJSON); !reflect.DeepEqual(create, want) {
		t.Fatalf("create body = %#v, want %#v", create, want)
	}
	fields := create["fields"].(map[string]any)
	if fields["customfield_number"] != json.Number("9007199254740993") {
		t.Fatalf("number = %#v", fields["customfield_number"])
	}
	if fields["customfield_enabled"] != true {
		t.Fatalf("boolean = %#v", fields["customfield_enabled"])
	}
	if value, ok := fields["customfield_cleared"]; !ok || value != nil {
		t.Fatalf("null = %#v, present=%v", value, ok)
	}
	if !reflect.DeepEqual(fields["customfield_multi"], []any{
		map[string]any{"id": "20001"},
		map[string]any{"id": "20002"},
	}) {
		t.Fatalf("multi-select = %#v", fields["customfield_multi"])
	}
	if fields["duedate"] != "2026-08-01" {
		t.Fatalf("date = %#v", fields["duedate"])
	}
	properties := create["properties"].([]any)
	propertyValue := properties[0].(map[string]any)["value"].(map[string]any)
	if propertyValue["attempt"] != json.Number("2") {
		t.Fatalf("property number = %#v", propertyValue["attempt"])
	}

	update := requests["PUT /rest/api/2/issue/ENG-2"]
	if want := decodeJSONMap(t, updateJSON); !reflect.DeepEqual(update, want) {
		t.Fatalf("update body = %#v, want %#v", update, want)
	}
	if update["fields"].(map[string]any)["customfield_number"] != json.Number("3.5") {
		t.Fatalf("update body = %#v", update)
	}
	transition := requests["POST /rest/api/2/issue/ENG-2/transitions"]
	if want := decodeJSONMap(t, transitionJSON); !reflect.DeepEqual(transition, want) {
		t.Fatalf("transition body = %#v, want %#v", transition, want)
	}
	if transition["transition"].(map[string]any)["id"] != "31" {
		t.Fatalf("transition body = %#v", transition)
	}
	if transition["fields"].(map[string]any)["resolution"].(map[string]any)["id"] != "1" {
		t.Fatalf("transition fields = %#v", transition)
	}
}

func TestMutationInputFromFileAndStdinProducesIdenticalDryRun(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMutationConfig(t, dir, "https://jira.example.com")
	input := `{"fields":{"customfield_10001":7,"labels":["one","two"]}}`
	inputPath := writeInputFile(t, dir, "update.json", input)

	var fileOutput bytes.Buffer
	fileApp := App{Stdout: &fileOutput, Stderr: io.Discard}
	if err := fileApp.Run([]string{
		"--config", configPath,
		"update", "ENG-1", "--input", inputPath, "--dry-run",
	}); err != nil {
		t.Fatal(err)
	}

	var stdinOutput bytes.Buffer
	stdinApp := App{
		Stdin:  strings.NewReader(input),
		Stdout: &stdinOutput,
		Stderr: io.Discard,
	}
	if err := stdinApp.Run([]string{
		"--config", configPath,
		"update", "ENG-1", "--input", "-", "--dry-run",
	}); err != nil {
		t.Fatal(err)
	}

	if fileOutput.String() != stdinOutput.String() {
		t.Fatalf("file=%s\nstdin=%s", fileOutput.String(), stdinOutput.String())
	}
}

func TestDryRunMakesZeroMutationRequests(t *testing.T) {
	mutationRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			mutationRequests++
		}
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := writeMutationConfig(t, dir, server.URL)
	inputBodies := map[string]string{
		"create":     `{"fields":{"project":{"key":"ENG"},"issuetype":{"name":"Task"},"summary":"Dry"}}`,
		"update":     `{"fields":{"summary":"Dry"}}`,
		"transition": `{"transition":{"id":"31"},"fields":{"resolution":{"id":"1"}}}`,
	}
	inputs := map[string]string{}
	for name, body := range inputBodies {
		inputs[name] = writeInputFile(t, dir, name+".json", body)
	}
	tests := []struct {
		name    string
		command []string
		method  string
		path    string
	}{
		{
			name:    "create",
			command: []string{"create", "--input", inputs["create"], "--dry-run"},
			method:  http.MethodPost,
			path:    "/rest/api/2/issue",
		},
		{
			name:    "update",
			command: []string{"update", "ENG-1", "--input", inputs["update"], "--dry-run"},
			method:  http.MethodPut,
			path:    "/rest/api/2/issue/ENG-1",
		},
		{
			name:    "transition",
			command: []string{"transition", "ENG-1", "--input", inputs["transition"], "--dry-run"},
			method:  http.MethodPost,
			path:    "/rest/api/2/issue/ENG-1/transitions",
		},
	}
	for _, tt := range tests {
		var stdout bytes.Buffer
		app := App{Stdout: &stdout, Stderr: io.Discard}
		args := append([]string{"--config", configPath}, tt.command...)
		if err := app.Run(args); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				DryRun  bool `json:"dryRun"`
				Request struct {
					Method string         `json:"method"`
					Path   string         `json:"path"`
					Body   map[string]any `json:"body"`
				} `json:"request"`
			} `json:"data"`
		}
		decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		decoder.UseNumber()
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if !envelope.OK || !envelope.Data.DryRun ||
			envelope.Data.Request.Method != tt.method ||
			envelope.Data.Request.Path != tt.path ||
			!reflect.DeepEqual(envelope.Data.Request.Body, decodeJSONMap(t, inputBodies[tt.name])) {
			t.Fatalf("dry-run envelope = %#v", envelope)
		}
	}
	if mutationRequests != 0 {
		t.Fatalf("mutation requests = %d", mutationRequests)
	}
}

func TestMutationInputRejectsMalformedOversizedAndAmbiguousData(t *testing.T) {
	dir := t.TempDir()
	validInput := writeInputFile(t, dir, "valid.json", `{"fields":{"summary":"Input"}}`)
	malformedInput := writeInputFile(t, dir, "malformed.json", `{"fields":`)
	wrongTypeInput := writeInputFile(t, dir, "wrong-type.json", `{"fields":[]}`)
	unknownInput := writeInputFile(t, dir, "unknown.json", `{"fields":{"summary":"Input"},"fieldz":{}}`)
	transitionInput := writeInputFile(t, dir, "transition.json", `{"transition":{"id":"31"}}`)

	tests := []struct {
		name  string
		app   App
		args  []string
		match string
	}{
		{
			name:  "malformed",
			app:   App{Stdout: io.Discard, Stderr: io.Discard},
			args:  []string{"create", "--input", malformedInput},
			match: "parse --input JSON",
		},
		{
			name: "oversized",
			app: App{
				Stdin:  strings.NewReader(strings.Repeat("x", maxMutationInputBytes+1)),
				Stdout: io.Discard,
				Stderr: io.Discard,
			},
			args:  []string{"update", "ENG-1", "--input", "-"},
			match: "exceeds",
		},
		{
			name:  "create ambiguity",
			app:   App{Stdout: io.Discard, Stderr: io.Discard},
			args:  []string{"create", "--input", validInput, "--summary", "Conflict"},
			match: "conflicts",
		},
		{
			name:  "wrong envelope type",
			app:   App{Stdout: io.Discard, Stderr: io.Discard},
			args:  []string{"update", "ENG-1", "--input", wrongTypeInput},
			match: "fields must be a JSON object",
		},
		{
			name:  "unknown envelope field",
			app:   App{Stdout: io.Discard, Stderr: io.Discard},
			args:  []string{"update", "ENG-1", "--input", unknownInput},
			match: `"fieldz" is not allowed`,
		},
		{
			name:  "transition ambiguity",
			app:   App{Stdout: io.Discard, Stderr: io.Discard},
			args:  []string{"transition", "ENG-1", "--input", transitionInput, "--to", "Done"},
			match: "conflicts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.app.Run(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.match) {
				t.Fatalf("error = %v, want %q", err, tt.match)
			}
		})
	}
}

func writeMutationConfig(t *testing.T, dir, baseURL string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	body := "[jira]\nbase_url = \"" + baseURL + "\"\ntoken = \"secret\"\ndeployment = \"data_center\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeInputFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeJSONMap(t *testing.T, body string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
