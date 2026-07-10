package cli

import "testing"

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
