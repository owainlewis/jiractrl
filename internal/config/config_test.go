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
