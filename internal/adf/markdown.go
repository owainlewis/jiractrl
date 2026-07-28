package adf

import (
	"regexp"
	"strconv"
	"strings"
)

var orderedItem = regexp.MustCompile(`^(\d+)\.\s+(.*)$`)

// FromMarkdown converts the documented jiractrl Markdown subset to an
// Atlassian Document Format document.
func FromMarkdown(markdown string) map[string]any {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	content := make([]any, 0)
	paragraph := make([]string, 0)

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		content = append(content, node("paragraph", inline(strings.Join(paragraph, " "))))
		paragraph = paragraph[:0]
	}

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushParagraph()
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			flushParagraph()
			language := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			i++
			code := make([]string, 0)
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			block := map[string]any{
				"type":    "codeBlock",
				"content": []any{textNode(strings.Join(code, "\n"))},
			}
			if language != "" {
				block["attrs"] = map[string]any{"language": language}
			}
			content = append(content, block)
			continue
		}

		if level, text, ok := heading(trimmed); ok {
			flushParagraph()
			content = append(content, map[string]any{
				"type":    "heading",
				"attrs":   map[string]any{"level": level},
				"content": inline(text),
			})
			i++
			continue
		}

		if _, ok := unordered(trimmed); ok {
			flushParagraph()
			items := make([]any, 0)
			for i < len(lines) {
				item, found := unordered(strings.TrimSpace(lines[i]))
				if !found {
					break
				}
				items = append(items, listItem(item))
				i++
			}
			content = append(content, node("bulletList", items))
			continue
		}

		if start, text, ok := ordered(trimmed); ok {
			flushParagraph()
			items := []any{listItem(text)}
			i++
			for i < len(lines) {
				_, item, found := ordered(strings.TrimSpace(lines[i]))
				if !found {
					break
				}
				items = append(items, listItem(item))
				i++
			}
			list := node("orderedList", items)
			if start != 1 {
				list["attrs"] = map[string]any{"order": start}
			}
			content = append(content, list)
			continue
		}

		paragraph = append(paragraph, trimmed)
		i++
	}
	flushParagraph()

	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": content,
	}
}

func heading(line string) (int, string, bool) {
	count := 0
	for count < len(line) && count < 6 && line[count] == '#' {
		count++
	}
	if count == 0 || count >= len(line) || line[count] != ' ' {
		return 0, "", false
	}
	return count, strings.TrimSpace(line[count:]), true
}

func unordered(line string) (string, bool) {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return strings.TrimSpace(line[2:]), true
	}
	return "", false
}

func ordered(line string) (int, string, bool) {
	match := orderedItem.FindStringSubmatch(line)
	if match == nil {
		return 0, "", false
	}
	start, _ := strconv.Atoi(match[1])
	return start, strings.TrimSpace(match[2]), true
}

func listItem(text string) map[string]any {
	return node("listItem", []any{node("paragraph", inline(text))})
}

func node(kind string, content []any) map[string]any {
	return map[string]any{"type": kind, "content": content}
}

func textNode(text string, marks ...map[string]any) map[string]any {
	value := map[string]any{"type": "text", "text": text}
	if len(marks) > 0 {
		values := make([]any, len(marks))
		for i, mark := range marks {
			values[i] = mark
		}
		value["marks"] = values
	}
	return value
}

func inline(text string) []any {
	content := make([]any, 0)
	for len(text) > 0 {
		switch {
		case strings.HasPrefix(text, "**"):
			if end := strings.Index(text[2:], "**"); end >= 0 {
				content = append(content, textNode(text[2:2+end], map[string]any{"type": "strong"}))
				text = text[2+end+2:]
				continue
			}
		case strings.HasPrefix(text, "`"):
			if end := strings.Index(text[1:], "`"); end >= 0 {
				content = append(content, textNode(text[1:1+end], map[string]any{"type": "code"}))
				text = text[1+end+1:]
				continue
			}
		case strings.HasPrefix(text, "["):
			if labelEnd := strings.Index(text, "]("); labelEnd > 0 {
				if hrefEnd := strings.Index(text[labelEnd+2:], ")"); hrefEnd >= 0 {
					label := text[1:labelEnd]
					href := text[labelEnd+2 : labelEnd+2+hrefEnd]
					content = append(content, textNode(label, map[string]any{
						"type":  "link",
						"attrs": map[string]any{"href": href},
					}))
					text = text[labelEnd+2+hrefEnd+1:]
					continue
				}
			}
		case strings.HasPrefix(text, "*"), strings.HasPrefix(text, "_"):
			marker := text[:1]
			if end := strings.Index(text[1:], marker); end >= 0 {
				content = append(content, textNode(text[1:1+end], map[string]any{"type": "em"}))
				text = text[1+end+1:]
				continue
			}
		}

		next := nextInlineMarker(text)
		if next == 0 {
			next = 1
		}
		content = append(content, textNode(text[:next]))
		text = text[next:]
	}
	return content
}

func nextInlineMarker(text string) int {
	next := len(text)
	for _, marker := range []string{"**", "`", "[", "*", "_"} {
		if index := strings.Index(text, marker); index >= 0 && index < next {
			next = index
		}
	}
	return next
}
