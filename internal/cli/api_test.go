package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIGetReturnsStructuredSameOriginResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/custom/1" ||
			r.URL.Query().Get("expand") != "all" ||
			r.Header.Get("Authorization") != "Bearer secret" ||
			r.Header.Get("X-Trace-ID") != "trace-1" {
			t.Fatalf("request = %s %s, headers = %#v", r.Method, r.URL.String(), r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","name":"custom"}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"api", "get", "/rest/custom/1?expand=all",
		"--header", "X-Trace-ID: trace-1", "--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("error = %v, stderr = %s", err, stderr.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status int            `json:"status"`
			JSON   bool           `json:"json"`
			Body   map[string]any `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Status != http.StatusOK ||
		!envelope.Data.JSON || envelope.Data.Body["name"] != "custom" {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestAPIRequestWritesExactInputWithConfirmation(t *testing.T) {
	input := "{\n  \"value\": 1\n}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		if r.Method != http.MethodPatch || r.URL.Path != "/rest/custom/1" ||
			r.Header.Get("Content-Type") != "application/json-patch+json" ||
			body.String() != input {
			t.Fatalf("request = %s %s, content type = %q, body = %q",
				r.Method, r.URL.Path, r.Header.Get("Content-Type"), body.String())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "request.json")
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"api", "request", "/rest/custom/1", "--method", "PATCH",
		"--input", inputPath, "--header", "Content-Type: application/json-patch+json",
		"--allow-write", "--json",
	}, &stdout, &stderr)
	if err != nil || !strings.Contains(stdout.String(), `"status": 204`) {
		t.Fatalf("error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
	}
}

func TestAPINonJSONResponsePreservesBytesAsBase64(t *testing.T) {
	payload := []byte{0x00, 0xff, 'A', '\n'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"api", "get", "/rest/binary", "--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			ContentType string `json:"contentType"`
			BodyBase64  string `json:"bodyBase64"`
			Bytes       int    `json:"bytes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(envelope.Data.BodyBase64)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ContentType != "application/octet-stream" ||
		envelope.Data.Bytes != len(payload) || !bytes.Equal(decoded, payload) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestAPIWriteWithoutConfirmationPerformsZeroRequests(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"api", "request", "/rest/write", "--method", "POST", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || requests != 0 ||
		!strings.Contains(stderr.String(), `"ok": false`) {
		t.Fatalf("error = %v, requests = %d, stderr = %s", err, requests, stderr.String())
	}
}

func TestAPIGetReturnsStructuredRetryMetadata(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Retry-After", "1")
		w.Header().Set("RateLimit-Reason", "jira-cost-based")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errorMessages":["slow down"]}`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "data_center")
	config, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteString("\n[retry]\nmax_attempts = 2\nbase_delay_ms = 0\nmax_delay_ms = 0\n"); err != nil {
		config.Close()
		t.Fatal(err)
	}
	if err := config.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err = Run([]string{
		"--config", configPath, "api", "get", "/rest/rate-limited", "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || requests != 2 {
		t.Fatalf("error = %v, requests = %d, stderr = %s", err, requests, stderr.String())
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
	if envelope.OK || envelope.Error.Kind != "rate_limited" ||
		envelope.Error.Status != http.StatusTooManyRequests ||
		envelope.Error.Retry.Attempts != 2 || !envelope.Error.Retry.Retryable ||
		envelope.Error.Retry.Reason != "jira-cost-based" {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestAPIUnsafePathAndHeadersPerformZeroRequests(t *testing.T) {
	tests := [][]string{
		{"api", "get", "https://evil.example/rest/api/3/myself", "--json"},
		{"api", "get", "//evil.example/rest/api/3/myself", "--json"},
		{"api", "get", "/%252e%252e/admin", "--json"},
		{"api", "get", "/rest/api/3/myself", "--header", "Authorization: attacker", "--json"},
		{"api", "get", "/rest/api/3/myself", "--header", "X-Forwarded-Host: evil.example", "--json"},
	}
	for _, command := range tests {
		t.Run(strings.Join(command, "_"), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", writeCommentsConfig(t, server.URL, "data_center")}, command...)
			err := Run(args, &stdout, &stderr)
			if err == nil || !IsReported(err) || requests != 0 {
				t.Fatalf("error = %v, requests = %d, stdout = %s, stderr = %s",
					err, requests, stdout.String(), stderr.String())
			}
		})
	}
}

func TestAPIInputValidationHappensBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()
	dir := t.TempDir()
	invalidPath := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalidPath, []byte(`{"broken":`), 0o600); err != nil {
		t.Fatal(err)
	}
	largePath := filepath.Join(dir, "large.json")
	large := append([]byte(`"`), bytes.Repeat([]byte("a"), maxRawAPIInputBytes)...)
	large = append(large, '"')
	if err := os.WriteFile(largePath, large, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeCommentsConfig(t, server.URL, "data_center")
	tests := [][]string{
		{"--config", configPath, "api", "request", "/rest/write", "--method", "POST", "--input", invalidPath, "--allow-write"},
		{"--config", configPath, "api", "request", "/rest/write", "--method", "POST", "--input", largePath, "--allow-write"},
		{"--config", configPath, "api", "get", "/rest/read", "--input", invalidPath},
		{"--config", configPath, "api", "request", "/rest/read", "--method", "CONNECT"},
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

func TestAPIRequestReadsJSONFromStdin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		if body.String() != `{"value":"stdin"}` {
			t.Fatalf("body = %q", body.String())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := App{
		Stdin: strings.NewReader(`{"value":"stdin"}`), Stdout: &stdout, Stderr: &bytes.Buffer{},
	}
	err := app.Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"api", "request", "/rest/write", "--method", "PUT",
		"--input", "-", "--allow-write", "--json",
	})
	if err != nil || !strings.Contains(stdout.String(), `"status": 204`) {
		t.Fatalf("error = %v, stdout = %s", err, stdout.String())
	}
}
