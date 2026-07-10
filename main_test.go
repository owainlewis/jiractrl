package main

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

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	config := `[jira]
base_url = "https://jira.example.com"
token = "secret"

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

	cfg, err := loadConfig(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.BaseURL != "https://jira.example.com" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Token != "secret" {
		t.Fatalf("Token = %q", cfg.Token)
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
