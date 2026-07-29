package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellExamplesHaveValidSyntax(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("public install and shell examples target macOS and Linux")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}

	root := filepath.Clean(filepath.Join("..", ".."))
	documents := []string{
		"README.md",
		"SPEC.md",
		"docs/agent-guide.md",
	}
	for _, document := range documents {
		t.Run(strings.ReplaceAll(document, "/", "_"), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, document))
			if err != nil {
				t.Fatal(err)
			}
			script := shellBlocks(string(body))
			path := filepath.Join(t.TempDir(), "examples.sh")
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command("bash", "-n", path).CombinedOutput()
			if err != nil {
				t.Fatalf("%s contains invalid shell syntax: %v\n%s\nscript:\n%s",
					document, err, output, script)
			}
		})
	}
}

func TestJiraExamplesUseMachineReadableOutput(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	documents := []string{
		"README.md",
		"SPEC.md",
		"docs/agent-guide.md",
	}
	for _, document := range documents {
		body, err := os.ReadFile(filepath.Join(root, document))
		if err != nil {
			t.Fatal(err)
		}
		script := strings.ReplaceAll(shellBlocks(string(body)), "\\\n", " ")
		for _, line := range strings.Split(script, "\n") {
			command := strings.TrimSpace(line)
			if !strings.HasPrefix(command, "jiractrl ") ||
				strings.HasPrefix(command, "jiractrl help") {
				continue
			}
			if !strings.Contains(command, "--json") && !strings.Contains(command, "--raw-json") {
				t.Errorf("%s example does not request machine-readable output: %s", document, command)
			}
		}
	}
}

func shellBlocks(markdown string) string {
	var script strings.Builder
	inShell := false
	for _, line := range strings.Split(markdown, "\n") {
		switch {
		case !inShell && (line == "```sh" || line == "```bash"):
			inShell = true
		case inShell && line == "```":
			inShell = false
			script.WriteByte('\n')
		case inShell:
			script.WriteString(line)
			script.WriteByte('\n')
		}
	}
	return script.String()
}
