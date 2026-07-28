package adf

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFromMarkdownBlocks(t *testing.T) {
	document := FromMarkdown(`# Heading

First paragraph
continues.

- one
- two

3. third
4. fourth

` + "```go\nfmt.Println(\"hello\")\n```")

	content := document["content"].([]any)
	kinds := make([]string, len(content))
	for i, item := range content {
		kinds[i] = item.(map[string]any)["type"].(string)
	}
	if got := strings.Join(kinds, ","); got != "heading,paragraph,bulletList,orderedList,codeBlock" {
		t.Fatalf("node types = %s", got)
	}
	if got := content[0].(map[string]any)["attrs"].(map[string]any)["level"]; got != 1 {
		t.Fatalf("heading level = %#v", got)
	}
	if got := content[3].(map[string]any)["attrs"].(map[string]any)["order"]; got != 3 {
		t.Fatalf("ordered list start = %#v", got)
	}
	if got := content[4].(map[string]any)["attrs"].(map[string]any)["language"]; got != "go" {
		t.Fatalf("code language = %#v", got)
	}
}

func TestFromMarkdownInlineMarks(t *testing.T) {
	document := FromMarkdown("Use **bold**, _italic_, `code`, and [Jira](https://jira.example.com).")
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"type":"strong"`,
		`"type":"em"`,
		`"type":"code"`,
		`"type":"link"`,
		`"href":"https://jira.example.com"`,
	} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("ADF missing %s: %s", fragment, data)
		}
	}
}

func TestFromMarkdownAlwaysReturnsDocument(t *testing.T) {
	document := FromMarkdown("")
	if document["type"] != "doc" || document["version"] != 1 {
		t.Fatalf("document = %#v", document)
	}
	if len(document["content"].([]any)) != 0 {
		t.Fatalf("content = %#v", document["content"])
	}
}
