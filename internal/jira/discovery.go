package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func (c *Client) Projects(ctx context.Context, query string, startAt, maxResults int) (*ProjectPage, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	if deployment == DeploymentCloud {
		path, err := c.PlatformPath(ctx, 3, "/project/search")
		if err != nil {
			return nil, err
		}
		q := url.Values{
			"startAt":    {strconv.Itoa(startAt)},
			"maxResults": {strconv.Itoa(maxResults)},
		}
		if strings.TrimSpace(query) != "" {
			q.Set("query", query)
		}
		var raw struct {
			StartAt    int       `json:"startAt"`
			MaxResults int       `json:"maxResults"`
			Total      int       `json:"total"`
			IsLast     bool      `json:"isLast"`
			Values     []Project `json:"values"`
		}
		if err := c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &raw); err != nil {
			return nil, err
		}
		return &ProjectPage{
			Projects: raw.Values,
			Page:     pageFromOffset(raw.StartAt, raw.MaxResults, len(raw.Values), raw.Total, raw.IsLast),
		}, nil
	}

	path, err := c.PlatformPath(ctx, 2, "/project")
	if err != nil {
		return nil, err
	}
	var projects []Project
	if err := c.doRead(ctx, http.MethodGet, path, nil, &projects); err != nil {
		return nil, err
	}
	if query = strings.ToLower(strings.TrimSpace(query)); query != "" {
		filtered := projects[:0]
		for _, project := range projects {
			if strings.Contains(strings.ToLower(project.Key), query) ||
				strings.Contains(strings.ToLower(project.Name), query) {
				filtered = append(filtered, project)
			}
		}
		projects = filtered
	}
	total := len(projects)
	if startAt > total {
		startAt = total
	}
	end := min(startAt+maxResults, total)
	values := append([]Project(nil), projects[startAt:end]...)
	return &ProjectPage{
		Projects: values,
		Page:     pageFromOffset(startAt, maxResults, len(values), total, end >= total),
	}, nil
}

func (c *Client) Project(ctx context.Context, key string) (*Project, error) {
	path, err := c.PlatformPath(ctx, 3, "/project/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var project Project
	if err := c.doRead(ctx, http.MethodGet, path, nil, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

func (c *Client) ProjectIssueTypes(ctx context.Context, project string, startAt, maxResults int) (*IssueTypePage, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	if deployment == DeploymentCloud {
		return c.cloudCreateIssueTypes(ctx, project, startAt, maxResults)
	}
	found, err := c.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	total := len(found.IssueTypes)
	if startAt > total {
		startAt = total
	}
	end := min(startAt+maxResults, total)
	values := append([]IssueType(nil), found.IssueTypes[startAt:end]...)
	return &IssueTypePage{
		IssueTypes: values,
		Page:       pageFromOffset(startAt, maxResults, len(values), total, end >= total),
	}, nil
}

func (c *Client) CreateMetadata(ctx context.Context, project, issueType string, startAt, maxResults int) (*MetadataResponse, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	if deployment == DeploymentCloud {
		types, err := c.allCloudCreateIssueTypes(ctx, project)
		if err != nil {
			return nil, err
		}
		selected, err := resolveIssueType(types, issueType)
		if err != nil {
			return nil, err
		}
		path, err := c.PlatformPath(ctx, 3, "/issue/createmeta/"+url.PathEscape(project)+"/issuetypes/"+url.PathEscape(selected.ID))
		if err != nil {
			return nil, err
		}
		q := url.Values{
			"startAt":    {strconv.Itoa(startAt)},
			"maxResults": {strconv.Itoa(maxResults)},
		}
		var raw struct {
			StartAt    int                      `json:"startAt"`
			MaxResults int                      `json:"maxResults"`
			Total      int                      `json:"total"`
			IsLast     bool                     `json:"isLast"`
			Fields     []cloudFieldMetadataWire `json:"fields"`
		}
		if err := c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &raw); err != nil {
			return nil, err
		}
		fields := make([]FieldMetadata, len(raw.Fields))
		for i, field := range raw.Fields {
			id := field.FieldID
			if id == "" {
				id = field.Key
			}
			fields[i] = FieldMetadata{
				ID: id, Name: field.Name, Required: field.Required, Schema: field.Schema,
				HasDefaultValue: field.HasDefaultValue, DefaultValue: field.DefaultValue,
				AllowedValues: field.AllowedValues,
			}
		}
		normalizeFields(fields)
		return &MetadataResponse{
			Project:   &Project{Key: project},
			IssueType: selected,
			Fields:    fields,
			Page:      pageFromOffset(raw.StartAt, raw.MaxResults, len(fields), raw.Total, raw.IsLast),
		}, nil
	}
	return c.dataCenterCreateMetadata(ctx, project, issueType, startAt, maxResults)
}

func (c *Client) EditMetadata(ctx context.Context, issue string) (*MetadataResponse, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/editmeta")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Fields map[string]fieldMetadataWire `json:"fields"`
	}
	if err := c.doRead(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	fields := fieldsFromMap(raw.Fields)
	total := len(fields)
	return &MetadataResponse{
		Issue:  issue,
		Fields: fields,
		Page:   pageFromOffset(0, total, total, total, true),
	}, nil
}

func (c *Client) AssignableUsers(ctx context.Context, project, issue, query string, startAt, maxResults int) (*UserPage, error) {
	path, err := c.PlatformPath(ctx, 3, "/user/assignable/search")
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"startAt":    {strconv.Itoa(startAt)},
		"maxResults": {strconv.Itoa(maxResults)},
	}
	if project != "" {
		q.Set("project", project)
	}
	if issue != "" {
		q.Set("issueKey", issue)
	}
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	if deployment == DeploymentCloud {
		q.Set("query", query)
	} else {
		q.Set("username", query)
	}
	var users []UserIdentity
	if err := c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &users); err != nil {
		return nil, err
	}
	hasMore := len(users) == maxResults
	if deployment == DeploymentCloud {
		// Cloud filters after selecting the requested window from the first
		// 1,000 users, so a short or empty page does not prove there are no
		// matches in a later window.
		hasMore = startAt+maxResults < 1000
	}
	page := DiscoveryPage{StartAt: startAt, MaxResults: maxResults, Returned: len(users), HasMore: hasMore}
	if hasMore {
		page.Next = startAt + maxResults
	}
	return &UserPage{Users: users, Page: page}, nil
}

type fieldMetadataWire struct {
	Name            string `json:"name"`
	Required        bool   `json:"required"`
	Schema          any    `json:"schema"`
	HasDefaultValue bool   `json:"hasDefaultValue"`
	DefaultValue    any    `json:"defaultValue"`
	AllowedValues   []any  `json:"allowedValues"`
}

type cloudFieldMetadataWire struct {
	FieldID string `json:"fieldId"`
	Key     string `json:"key"`
	fieldMetadataWire
}

func (c *Client) cloudCreateIssueTypes(ctx context.Context, project string, startAt, maxResults int) (*IssueTypePage, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/createmeta/"+url.PathEscape(project)+"/issuetypes")
	if err != nil {
		return nil, err
	}
	q := url.Values{"startAt": {strconv.Itoa(startAt)}, "maxResults": {strconv.Itoa(maxResults)}}
	var raw struct {
		StartAt    int         `json:"startAt"`
		MaxResults int         `json:"maxResults"`
		Total      int         `json:"total"`
		IsLast     bool        `json:"isLast"`
		IssueTypes []IssueType `json:"issueTypes"`
	}
	if err := c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	return &IssueTypePage{
		IssueTypes: raw.IssueTypes,
		Page:       pageFromOffset(raw.StartAt, raw.MaxResults, len(raw.IssueTypes), raw.Total, raw.IsLast),
	}, nil
}

func (c *Client) allCloudCreateIssueTypes(ctx context.Context, project string) ([]IssueType, error) {
	var result []IssueType
	startAt := 0
	for pages := 0; pages < 100; pages++ {
		page, err := c.cloudCreateIssueTypes(ctx, project, startAt, 50)
		if err != nil {
			return nil, err
		}
		result = append(result, page.IssueTypes...)
		if !page.Page.HasMore {
			return result, nil
		}
		startAt = page.Page.Next
	}
	return nil, fmt.Errorf("issue type discovery exceeded the 100-page safety budget")
}

func (c *Client) dataCenterCreateMetadata(ctx context.Context, project, issueType string, startAt, maxResults int) (*MetadataResponse, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue/createmeta")
	if err != nil {
		return nil, err
	}
	q := url.Values{"projectKeys": {project}, "expand": {"projects.issuetypes.fields"}}
	var raw struct {
		Projects []struct {
			ID         string `json:"id"`
			Key        string `json:"key"`
			Name       string `json:"name"`
			IssueTypes []struct {
				IssueType
				Fields map[string]fieldMetadataWire `json:"fields"`
			} `json:"issuetypes"`
		} `json:"projects"`
	}
	if err := c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	if len(raw.Projects) == 0 {
		return nil, fmt.Errorf("project %q was not returned by create metadata", project)
	}
	types := make([]IssueType, len(raw.Projects[0].IssueTypes))
	for i := range raw.Projects[0].IssueTypes {
		types[i] = raw.Projects[0].IssueTypes[i].IssueType
	}
	selected, err := resolveIssueType(types, issueType)
	if err != nil {
		return nil, err
	}
	var fields []FieldMetadata
	for i := range raw.Projects[0].IssueTypes {
		if raw.Projects[0].IssueTypes[i].ID == selected.ID {
			fields = fieldsFromMap(raw.Projects[0].IssueTypes[i].Fields)
			break
		}
	}
	total := len(fields)
	if startAt > total {
		startAt = total
	}
	end := min(startAt+maxResults, total)
	return &MetadataResponse{
		Project:   &Project{ID: raw.Projects[0].ID, Key: raw.Projects[0].Key, Name: raw.Projects[0].Name},
		IssueType: selected,
		Fields:    append([]FieldMetadata(nil), fields[startAt:end]...),
		Page:      pageFromOffset(startAt, maxResults, end-startAt, total, end >= total),
	}, nil
}

func resolveIssueType(types []IssueType, value string) (*IssueType, error) {
	for i := range types {
		if types[i].ID == value {
			return &types[i], nil
		}
	}
	var matches []IssueType
	for _, issueType := range types {
		if strings.EqualFold(issueType.Name, value) {
			matches = append(matches, issueType)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		return nil, &AmbiguousMatchError{Value: value, Candidates: matches}
	}
	return nil, fmt.Errorf("issue type %q was not found", value)
}

func fieldsFromMap(values map[string]fieldMetadataWire) []FieldMetadata {
	fields := make([]FieldMetadata, 0, len(values))
	for id, field := range values {
		allowed := field.AllowedValues
		if allowed == nil {
			allowed = []any{}
		}
		fields = append(fields, FieldMetadata{
			ID: id, Name: field.Name, Required: field.Required, Schema: field.Schema,
			HasDefaultValue: field.HasDefaultValue, DefaultValue: field.DefaultValue,
			AllowedValues: allowed,
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].ID < fields[j].ID })
	return fields
}

func normalizeFields(fields []FieldMetadata) {
	for i := range fields {
		if fields[i].AllowedValues == nil {
			fields[i].AllowedValues = []any{}
		}
	}
}

func pageFromOffset(startAt, maxResults, returned, total int, isLast bool) DiscoveryPage {
	hasMore := !isLast && startAt+returned < total
	page := DiscoveryPage{
		StartAt: startAt, MaxResults: maxResults, Returned: returned,
		Total: &total, HasMore: hasMore,
	}
	if hasMore {
		page.Next = startAt + returned
	}
	return page
}
