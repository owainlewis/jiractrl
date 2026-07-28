package jira

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (c *Client) Changelog(ctx context.Context, issue string, startAt, maxResults int, fields []string) (*ChangelogPage, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/changelog")
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"startAt":    {strconv.Itoa(startAt)},
		"maxResults": {strconv.Itoa(maxResults)},
	}
	var raw struct {
		StartAt    int         `json:"startAt"`
		MaxResults int         `json:"maxResults"`
		Total      int         `json:"total"`
		IsLast     bool        `json:"isLast"`
		Values     []Changelog `json:"values"`
		Histories  []Changelog `json:"histories"`
	}
	if err := c.doRead(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	histories := raw.Values
	if histories == nil {
		histories = raw.Histories
	}
	scanned := len(histories)
	normalizedFields := normalizeChangeFields(fields)
	if len(normalizedFields) > 0 {
		filtered := make([]Changelog, 0, len(histories))
		for _, history := range histories {
			items := history.Items[:0]
			for _, item := range history.Items {
				if normalizedFields[strings.ToLower(item.Field)] ||
					normalizedFields[strings.ToLower(item.FieldID)] {
					items = append(items, item)
				}
			}
			if len(items) > 0 {
				history.Items = items
				filtered = append(filtered, history)
			}
		}
		histories = filtered
	}
	fieldNames := make([]string, 0, len(normalizedFields))
	for _, field := range fields {
		if value := strings.TrimSpace(field); value != "" {
			fieldNames = append(fieldNames, value)
		}
	}
	page := pageFromOffset(raw.StartAt, raw.MaxResults, len(histories), raw.Total,
		raw.IsLast || raw.StartAt+scanned >= raw.Total)
	if page.HasMore {
		page.Next = raw.StartAt + scanned
	}
	return &ChangelogPage{
		Histories: histories,
		Page:      page,
		Scanned:   scanned,
		Fields:    fieldNames,
	}, nil
}

func normalizeChangeFields(fields []string) map[string]bool {
	result := make(map[string]bool, len(fields))
	for _, field := range fields {
		if value := strings.ToLower(strings.TrimSpace(field)); value != "" {
			result[value] = true
		}
	}
	return result
}
