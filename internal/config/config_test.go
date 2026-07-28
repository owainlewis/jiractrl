package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigWithProfiles(t *testing.T) {
	t.Setenv("JIRACTRL_BASE_URL", "")
	t.Setenv("JIRACTRL_TOKEN", "")
	t.Setenv("JIRA_BASE_URL", "")
	t.Setenv("JIRA_PAT", "")
	t.Setenv("JIRA_TOKEN", "")
	t.Setenv("JIRACTRL_EMAIL", "")
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRACTRL_DEPLOYMENT", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	config := `[jira]
base_url = "https://jira.example.com"
token = "secret"
email = "agent@example.com"
deployment = "data_center"

[defaults]
max_results = 25
output = "json"

[retry]
max_attempts = 4
base_delay_ms = 25
max_delay_ms = 250

[profiles.my_open]
jql = "assignee = currentUser() ORDER BY updated DESC"
fields = ["summary", "status", "updated"]
max_results = 10
`
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.BaseURL != "https://jira.example.com" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Token != "secret" {
		t.Fatalf("Token = %q", cfg.Token)
	}
	if cfg.Email != "agent@example.com" {
		t.Fatalf("Email = %q", cfg.Email)
	}
	if cfg.Deployment != "data_center" {
		t.Fatalf("Deployment = %q", cfg.Deployment)
	}
	if cfg.DefaultMaxResults != 25 {
		t.Fatalf("DefaultMaxResults = %d", cfg.DefaultMaxResults)
	}
	if cfg.RetryMaxAttempts != 4 || cfg.RetryBaseDelay != 25*time.Millisecond || cfg.RetryMaxDelay != 250*time.Millisecond {
		t.Fatalf("retry config = %d %s %s", cfg.RetryMaxAttempts, cfg.RetryBaseDelay, cfg.RetryMaxDelay)
	}
	p := cfg.Profiles["my_open"]
	if p.JQL == "" {
		t.Fatal("profile JQL was empty")
	}
	if p.MaxResults != 10 {
		t.Fatalf("profile MaxResults = %d", p.MaxResults)
	}
	if len(p.Fields) != 3 || p.Fields[2] != "updated" {
		t.Fatalf("profile Fields = %#v", p.Fields)
	}
}

func TestLoadRejectsInvalidDeploymentOverride(t *testing.T) {
	t.Setenv("JIRACTRL_TOKEN", "secret")
	t.Setenv("JIRACTRL_DEPLOYMENT", "hosted")
	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml"), time.Second); err == nil {
		t.Fatal("expected invalid deployment error")
	}
}

func TestLoadAllowsZeroRetryDelaysAndRejectsZeroAttempts(t *testing.T) {
	t.Setenv("JIRACTRL_TOKEN", "")
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `[jira]
token = "secret"

[retry]
max_attempts = 2
base_delay_ms = 0
max_delay_ms = 0
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetryBaseDelay != 0 || cfg.RetryMaxDelay != 0 {
		t.Fatalf("delays = %s %s", cfg.RetryBaseDelay, cfg.RetryMaxDelay)
	}

	body = `[jira]
token = "secret"

[retry]
max_attempts = 0
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, time.Second); err == nil {
		t.Fatal("expected zero attempts to be rejected")
	}
}
