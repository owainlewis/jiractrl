package cli

import (
	"bytes"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owainlewis/jiractrl/internal/jira"
)

func TestExitCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "local", err: errors.New("bad arguments"), want: 1},
		{name: "auth", err: &jira.Error{StatusCode: http.StatusUnauthorized}, want: 2},
		{name: "forbidden", err: &jira.Error{StatusCode: http.StatusForbidden}, want: 2},
		{name: "not found", err: &jira.Error{StatusCode: http.StatusNotFound}, want: 3},
		{name: "validation", err: &jira.Error{StatusCode: http.StatusBadRequest}, want: 4},
		{name: "server", err: &jira.Error{StatusCode: http.StatusBadGateway}, want: 5},
		{name: "rate limited", err: &jira.Error{StatusCode: http.StatusTooManyRequests}, want: 6},
		{name: "conflict", err: &jira.Error{StatusCode: http.StatusConflict}, want: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Fatalf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDryRunSelectsJSONErrorsOnlyForMutationCommands(t *testing.T) {
	for _, name := range []string{"JIRACTRL_TOKEN", "JIRA_PAT", "JIRA_TOKEN"} {
		t.Setenv(name, "")
	}
	configPath := filepath.Join(t.TempDir(), "missing.toml")

	var mutationStdout, mutationStderr bytes.Buffer
	err := Run([]string{
		"--config", configPath,
		"update", "ENG-1", "--summary", "Dry", "--dry-run",
	}, &mutationStdout, &mutationStderr)
	if err == nil || !IsReported(err) || !strings.Contains(mutationStderr.String(), `"ok": false`) {
		t.Fatalf("mutation error = %v, stderr = %s", err, mutationStderr.String())
	}

	var triageStdout, triageStderr bytes.Buffer
	err = Run([]string{
		"--config", configPath,
		"triage", "--jql", "project = ENG", "--dry-run",
	}, &triageStdout, &triageStderr)
	if err == nil || IsReported(err) {
		t.Fatalf("triage error = %v", err)
	}
	if triageStderr.Len() != 0 {
		t.Fatalf("triage stderr = %s", triageStderr.String())
	}
}
