package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBulkUpdateReportsEveryMixedResultInOrder(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/rest/api/2/issue/ENG-1", "/rest/api/2/issue/ENG-3":
			w.WriteHeader(http.StatusNoContent)
		case "/rest/api/2/issue/ENG-2":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorMessages":["invalid update"],"errors":{"summary":"required"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := writeMutationConfig(t, dir, server.URL)
	inputPath := writeInputFile(t, dir, "updates.json", `[
		{"identity":"first","issue":"ENG-1","fields":{"summary":"One"}},
		{"identity":"second","issue":"ENG-2","fields":{"summary":"Two"}},
		{"identity":"third","issue":"ENG-3","fields":{"summary":"Three"}}
	]`)

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "bulk", "update", "--input", inputPath, "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) {
		t.Fatalf("error = %v, want reported partial failure", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
	var envelope bulkEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout = %s: %v", stdout.String(), err)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.Kind != "partial_failure" {
		t.Fatalf("envelope = %#v", envelope)
	}
	result := envelope.Data
	if result.Requested != 3 || result.Processed != 3 || result.Succeeded != 2 ||
		result.Failed != 1 || result.Skipped != 0 || result.Stopped || result.Complete {
		t.Fatalf("result = %#v", result)
	}
	if got := []string{
		result.Results[0].Identity,
		result.Results[1].Identity,
		result.Results[2].Identity,
	}; !reflect.DeepEqual(got, []string{"first", "second", "third"}) {
		t.Fatalf("identities = %#v", got)
	}
	if result.Results[1].Error == nil ||
		result.Results[1].Error.Status != http.StatusBadRequest ||
		result.Results[1].Error.Fields["summary"] != "required" {
		t.Fatalf("failed item = %#v", result.Results[1])
	}
	if !reflect.DeepEqual(requested, []string{
		"/rest/api/2/issue/ENG-1",
		"/rest/api/2/issue/ENG-2",
		"/rest/api/2/issue/ENG-3",
	}) {
		t.Fatalf("requested = %#v", requested)
	}
}

func TestBulkCreateAcceptsJSONL(t *testing.T) {
	var summaries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		fields := body["fields"].(map[string]any)
		summaries = append(summaries, fields["summary"].(string))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"` + fields["summary"].(string) + `","key":"ENG-1"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	inputPath := writeInputFile(t, dir, "creates.jsonl", `
{"identity":"alpha","fields":{"project":{"key":"ENG"},"issuetype":{"name":"Task"},"summary":"One"}}

{"identity":"beta","fields":{"project":{"key":"ENG"},"issuetype":{"name":"Task"},"summary":"Two"}}
`)
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeMutationConfig(t, dir, server.URL),
		"bulk", "create", "--input", inputPath, "--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("error = %v, stderr = %s", err, stderr.String())
	}
	var envelope bulkEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Succeeded != 2 ||
		envelope.Data.Results[0].Identity != "alpha" ||
		envelope.Data.Results[1].Identity != "beta" {
		t.Fatalf("envelope = %#v", envelope)
	}
	if !reflect.DeepEqual(summaries, []string{"One", "Two"}) {
		t.Fatalf("summaries = %#v", summaries)
	}
}

func TestBulkRejectsLimitAndInvalidLaterItemBeforeNetwork(t *testing.T) {
	tests := []struct {
		name  string
		input string
		args  []string
		match string
	}{
		{
			name: "hard limit",
			input: `[
				{"issue":"ENG-1","fields":{"summary":"One"}},
				{"issue":"ENG-2","fields":{"summary":"Two"}}
			]`,
			args:  []string{"--max-items", "1"},
			match: "exceeding --max-items 1",
		},
		{
			name: "invalid later item",
			input: `[
				{"issue":"ENG-1","fields":{"summary":"One"}},
				{"identity":"bad","fields":{"summary":"No issue"}}
			]`,
			match: "bulk item 1: update requires issue",
		},
		{
			name:  "compiled ceiling",
			input: `[{"issue":"ENG-1","fields":{"summary":"One"}}]`,
			args:  []string{"--max-items", "1001"},
			match: "--max-items must be between 1 and 1000",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			dir := t.TempDir()
			inputPath := writeInputFile(t, dir, "input.json", test.input)
			args := []string{
				"--config", writeMutationConfig(t, dir, server.URL),
				"bulk", "update", "--input", inputPath,
			}
			args = append(args, test.args...)
			err := (App{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Run(args)
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("error = %v, want %q", err, test.match)
			}
			if requests != 0 {
				t.Fatalf("requests = %d, want 0", requests)
			}
		})
	}
}

func TestBulkDryRunPlansFullBatchWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	inputPath := writeInputFile(t, dir, "transitions.json", `[
		{"identity":"a","issue":"ENG-1","transition":{"id":"31"}},
		{"identity":"b","issue":"ENG-2","transition":{"id":"41"},"fields":{"resolution":{"name":"Done"}}}
	]`)
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeMutationConfig(t, dir, "http://127.0.0.1:1"),
		"bulk", "transition", "--input", inputPath, "--dry-run",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("error = %v, stderr = %s", err, stderr.String())
	}
	var envelope bulkEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout = %s: %v", stdout.String(), err)
	}
	if !envelope.OK || !envelope.Data.DryRun || envelope.Data.Processed != 2 ||
		envelope.Data.Succeeded != 2 {
		t.Fatalf("envelope = %#v", envelope)
	}
	for i, item := range envelope.Data.Results {
		if item.Status != "planned" || !item.Success {
			t.Fatalf("result %d = %#v", i, item)
		}
		request := item.Data.(map[string]any)
		if request["method"] != http.MethodPost ||
			!strings.Contains(request["path"].(string), item.Issue+"/transitions") {
			t.Fatalf("request = %#v", request)
		}
	}
}

func TestBulkStopsAfterAmbiguousTransportFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}))
	defer server.Close()

	dir := t.TempDir()
	inputPath := writeInputFile(t, dir, "updates.jsonl", strings.Join([]string{
		`{"identity":"first","issue":"ENG-1","fields":{"summary":"One"}}`,
		`{"identity":"second","issue":"ENG-2","fields":{"summary":"Two"}}`,
		`{"identity":"third","issue":"ENG-3","fields":{"summary":"Three"}}`,
	}, "\n"))
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeMutationConfig(t, dir, server.URL),
		"bulk", "update", "--input", inputPath, "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) {
		t.Fatalf("error = %v, want reported partial failure", err)
	}
	var envelope bulkEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	result := envelope.Data
	if result.Processed != 1 || result.Failed != 1 || result.Skipped != 2 ||
		!result.Stopped || result.Complete || len(result.Results) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if result.Results[0].Error == nil || result.Results[0].Error.Kind != "transport" {
		t.Fatalf("first result = %#v", result.Results[0])
	}
	for _, item := range result.Results[1:] {
		if item.Status != "skipped" || item.Error == nil || item.Error.Kind != "skipped" {
			t.Fatalf("skipped result = %#v", item)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestBulkStopsAfterAmbiguousHTTPResponse(t *testing.T) {
	for _, status := range []int{
		http.StatusRequestTimeout,
		499,
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"errorMessages":["ambiguous response"]}`))
			}))
			defer server.Close()

			dir := t.TempDir()
			inputPath := writeInputFile(t, dir, "updates.json", `[
				{"identity":"first","issue":"ENG-1","fields":{"summary":"One"}},
				{"identity":"second","issue":"ENG-2","fields":{"summary":"Two"}}
			]`)
			var stdout, stderr bytes.Buffer
			err := Run([]string{
				"--config", writeMutationConfig(t, dir, server.URL),
				"bulk", "update", "--input", inputPath, "--json",
			}, &stdout, &stderr)
			if err == nil || !IsReported(err) {
				t.Fatalf("error = %v, want reported partial failure", err)
			}
			var envelope bulkEnvelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			result := envelope.Data
			if requests != 1 || !result.Stopped || result.Processed != 1 ||
				result.Failed != 1 || result.Skipped != 1 ||
				result.Results[0].Error.Status != status ||
				result.Results[1].Status != "skipped" {
				t.Fatalf("requests = %d, result = %#v", requests, result)
			}
		})
	}
}

func TestBulkTextPartialOutputIncludesJiraError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["invalid update"]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	inputPath := writeInputFile(t, dir, "update.json",
		`[{"identity":"source-1","issue":"ENG-1","fields":{"summary":"One"}}]`)
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeMutationConfig(t, dir, server.URL),
		"bulk", "update", "--input", inputPath,
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) {
		t.Fatalf("error = %v, want reported partial failure", err)
	}
	if !strings.Contains(stdout.String(), "source-1  failed") ||
		!strings.Contains(stdout.String(), "HTTP 400:") ||
		!strings.Contains(stdout.String(), "invalid update") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestBulkReadsInputFromStdin(t *testing.T) {
	var stdout bytes.Buffer
	app := App{
		Stdin:  strings.NewReader(`{"issue":"ENG-1","fields":{"summary":"One"}}`),
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	}
	dir := t.TempDir()
	err := app.Run([]string{
		"--config", writeMutationConfig(t, dir, "http://127.0.0.1:1"),
		"bulk", "update", "--input", "-", "--dry-run",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"identity": "item-0"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestBulkDryRunValidationErrorUsesJSONContract(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "missing.json")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeMutationConfig(t, dir, "http://127.0.0.1:1"),
		"bulk", "create", "--input", inputPath, "--dry-run",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), `"ok": false`) {
		t.Fatalf("error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
	}
}
