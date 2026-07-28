package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	SoftwareReadPageLimit   = 100
	SoftwareWriteIssueLimit = 50
)

type softwareValuePage[T any] struct {
	StartAt    int  `json:"startAt"`
	MaxResults int  `json:"maxResults"`
	Total      *int `json:"total"`
	IsLast     bool `json:"isLast"`
	Values     []T  `json:"values"`
}

type softwareIssueResponse struct {
	StartAt       int        `json:"startAt"`
	MaxResults    int        `json:"maxResults"`
	Total         *int       `json:"total"`
	IsLast        *bool      `json:"isLast"`
	NextPageToken string     `json:"nextPageToken"`
	Issues        []RawIssue `json:"issues"`
}

func (c *Client) SoftwareBoards(ctx context.Context, name, boardType, project string, startAt, maxResults int) (*SoftwareBoardPage, error) {
	if err := validateSoftwarePage(startAt, maxResults); err != nil {
		return nil, err
	}
	boardType = strings.TrimSpace(strings.ToLower(boardType))
	if boardType != "" && boardType != "scrum" && boardType != "kanban" {
		return nil, &ValidationError{Field: "type", Message: "must be scrum or kanban"}
	}
	if err := c.RequireSoftware(ctx); err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("startAt", strconv.Itoa(startAt))
	query.Set("maxResults", strconv.Itoa(maxResults))
	if strings.TrimSpace(name) != "" {
		query.Set("name", name)
	}
	if strings.TrimSpace(boardType) != "" {
		query.Set("type", boardType)
	}
	if strings.TrimSpace(project) != "" {
		query.Set("projectKeyOrId", project)
	}
	var raw softwareValuePage[SoftwareBoard]
	if err := c.doRead(ctx, http.MethodGet, "/rest/agile/1.0/board?"+query.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	return &SoftwareBoardPage{Boards: raw.Values, Page: normalizeSoftwareOffsetPage(raw.StartAt, raw.MaxResults, raw.Total, raw.IsLast, len(raw.Values))}, nil
}

func (c *Client) SoftwareBoard(ctx context.Context, boardID int64) (*SoftwareBoard, error) {
	if boardID < 1 {
		return nil, &ValidationError{Field: "board", Message: "must be a positive integer"}
	}
	if err := c.RequireSoftware(ctx); err != nil {
		return nil, err
	}
	var result SoftwareBoard
	err := c.doRead(ctx, http.MethodGet, fmt.Sprintf("/rest/agile/1.0/board/%d", boardID), nil, &result)
	return &result, err
}

func (c *Client) SoftwareBoardSprints(ctx context.Context, boardID int64, state string, startAt, maxResults int) (*SoftwareSprintPage, error) {
	if boardID < 1 {
		return nil, &ValidationError{Field: "board", Message: "must be a positive integer"}
	}
	if err := validateSoftwarePage(startAt, maxResults); err != nil {
		return nil, err
	}
	state = strings.TrimSpace(strings.ToLower(state))
	if err := validateSprintStates(state); err != nil {
		return nil, err
	}
	if err := c.RequireSoftware(ctx); err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("startAt", strconv.Itoa(startAt))
	query.Set("maxResults", strconv.Itoa(maxResults))
	if strings.TrimSpace(state) != "" {
		query.Set("state", state)
	}
	var raw softwareValuePage[SoftwareSprint]
	path := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?%s", boardID, query.Encode())
	if err := c.doRead(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	return &SoftwareSprintPage{Sprints: raw.Values, Page: normalizeSoftwareOffsetPage(raw.StartAt, raw.MaxResults, raw.Total, raw.IsLast, len(raw.Values))}, nil
}

func (c *Client) SoftwareSprint(ctx context.Context, sprintID int64) (*SoftwareSprint, error) {
	if sprintID < 1 {
		return nil, &ValidationError{Field: "sprint", Message: "must be a positive integer"}
	}
	if err := c.RequireSoftware(ctx); err != nil {
		return nil, err
	}
	var result SoftwareSprint
	err := c.doRead(ctx, http.MethodGet, fmt.Sprintf("/rest/agile/1.0/sprint/%d", sprintID), nil, &result)
	return &result, err
}

func (c *Client) SoftwareBoardIssues(ctx context.Context, boardID int64, backlog bool, options SoftwareIssueOptions) (*SoftwareIssuePage, error) {
	if boardID < 1 {
		return nil, &ValidationError{Field: "board", Message: "must be a positive integer"}
	}
	resource := fmt.Sprintf("/board/%d/issue", boardID)
	if backlog {
		resource = fmt.Sprintf("/board/%d/backlog", boardID)
	}
	return c.softwareIssues(ctx, resource, options)
}

func (c *Client) SoftwareSprintIssues(ctx context.Context, sprintID int64, options SoftwareIssueOptions) (*SoftwareIssuePage, error) {
	if sprintID < 1 {
		return nil, &ValidationError{Field: "sprint", Message: "must be a positive integer"}
	}
	return c.softwareIssues(ctx, fmt.Sprintf("/sprint/%d/issue", sprintID), options)
}

func (c *Client) softwareIssues(ctx context.Context, resource string, options SoftwareIssueOptions) (*SoftwareIssuePage, error) {
	if err := validateSoftwarePage(0, options.MaxResults); err != nil {
		return nil, err
	}
	if err := c.RequireSoftware(ctx); err != nil {
		return nil, err
	}
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("maxResults", strconv.Itoa(options.MaxResults))
	if strings.TrimSpace(options.JQL) != "" {
		query.Set("jql", options.JQL)
	}
	if len(options.Fields) > 0 {
		query.Set("fields", strings.Join(options.Fields, ","))
	}
	prefix := "/rest/agile/1.0"
	if deployment == DeploymentCloud {
		prefix = "/rest/software/1.0"
		if strings.TrimSpace(options.Cursor) != "" {
			query.Set("nextPageToken", options.Cursor)
		}
	} else {
		startAt, err := parseSoftwareCursor(options.Cursor)
		if err != nil {
			return nil, err
		}
		query.Set("startAt", strconv.Itoa(startAt))
	}

	var raw softwareIssueResponse
	if err := c.doRead(ctx, http.MethodGet, prefix+resource+"?"+query.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	page, err := normalizeSoftwareIssuePage(deployment, raw)
	if err != nil {
		return nil, err
	}
	return &SoftwareIssuePage{Issues: raw.Issues, Page: page}, nil
}

func (c *Client) MoveIssuesToSprint(ctx context.Context, sprintID int64, issues []string, before, after string, rankFieldID int64) (*SoftwareWriteReceipt, error) {
	if sprintID < 1 {
		return nil, &ValidationError{Field: "sprint", Message: "must be a positive integer"}
	}
	payload, cleanIssues, err := softwareMovePayload(issues, before, after, rankFieldID)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/rest/agile/1.0/sprint/%d/issue", sprintID)
	return c.softwareWrite(ctx, "move_to_sprint", path, http.MethodPost, cleanIssues, payload)
}

func (c *Client) MoveIssuesToBacklog(ctx context.Context, boardID int64, issues []string) (*SoftwareWriteReceipt, error) {
	if boardID < 0 {
		return nil, &ValidationError{Field: "board", Message: "must be a positive integer"}
	}
	cleanIssues, err := validateSoftwareIssues(issues)
	if err != nil {
		return nil, err
	}
	path := "/rest/agile/1.0/backlog/issue"
	if boardID > 0 {
		deployment, err := c.Deployment(ctx)
		if err != nil {
			return nil, err
		}
		if deployment != DeploymentCloud {
			return nil, &UnsupportedCapabilityError{
				Capability: "board-scoped backlog move",
				Deployment: deployment,
			}
		}
		path = fmt.Sprintf("/rest/agile/1.0/backlog/%d/issue", boardID)
	}
	return c.softwareWrite(ctx, "move_to_backlog", path, http.MethodPost, cleanIssues, map[string]any{"issues": cleanIssues})
}

func (c *Client) RankIssues(ctx context.Context, issues []string, before, after string, rankFieldID int64) (*SoftwareWriteReceipt, error) {
	payload, cleanIssues, err := softwareRankPayload(issues, before, after, rankFieldID)
	if err != nil {
		return nil, err
	}
	return c.softwareWrite(ctx, "rank", "/rest/agile/1.0/issue/rank", http.MethodPut, cleanIssues, payload)
}

func (c *Client) EstimateIssue(ctx context.Context, issue string, boardID int64, value string) (json.RawMessage, error) {
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return nil, &ValidationError{Field: "issue", Message: "must not be empty"}
	}
	if boardID < 1 {
		return nil, &ValidationError{Field: "board", Message: "must be a positive integer"}
	}
	if strings.TrimSpace(value) == "" {
		return nil, &ValidationError{Field: "value", Message: "must not be empty"}
	}
	if err := c.RequireSoftware(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/rest/agile/1.0/issue/%s/estimation?boardId=%d", url.PathEscape(issue), boardID)
	var result json.RawMessage
	err := c.do(ctx, http.MethodPut, path, map[string]string{"value": value}, &result)
	return result, err
}

func (c *Client) softwareWrite(ctx context.Context, operation, path, method string, issues []string, payload map[string]any) (*SoftwareWriteReceipt, error) {
	if err := c.RequireSoftware(ctx); err != nil {
		return nil, err
	}
	status, details, err := c.doRaw(ctx, method, path, payload)
	if err != nil {
		return nil, err
	}
	partial := status == http.StatusMultiStatus
	return &SoftwareWriteReceipt{
		Operation: operation,
		Issues:    issues,
		Accepted:  !partial,
		Partial:   partial,
		Details:   details,
	}, nil
}

func softwareRankPayload(issues []string, before, after string, rankFieldID int64) (map[string]any, []string, error) {
	payload, cleanIssues, err := softwareMovePayload(issues, before, after, rankFieldID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(before) == "" && strings.TrimSpace(after) == "" {
		return nil, nil, &ValidationError{Field: "rank", Message: "set exactly one of before or after"}
	}
	return payload, cleanIssues, nil
}

func softwareMovePayload(issues []string, before, after string, rankFieldID int64) (map[string]any, []string, error) {
	cleanIssues, err := validateSoftwareIssues(issues)
	if err != nil {
		return nil, nil, err
	}
	before = strings.TrimSpace(before)
	after = strings.TrimSpace(after)
	if before != "" && after != "" {
		return nil, nil, &ValidationError{Field: "rank", Message: "set at most one of before or after"}
	}
	payload := map[string]any{"issues": cleanIssues}
	if before != "" {
		payload["rankBeforeIssue"] = before
	} else if after != "" {
		payload["rankAfterIssue"] = after
	}
	if rankFieldID < 0 {
		return nil, nil, &ValidationError{Field: "rank-field", Message: "must be non-negative"}
	}
	if rankFieldID > 0 {
		payload["rankCustomFieldId"] = rankFieldID
	}
	return payload, cleanIssues, nil
}

func validateSoftwareIssues(issues []string) ([]string, error) {
	if len(issues) == 0 {
		return nil, &ValidationError{Field: "issue", Message: "provide at least one issue"}
	}
	if len(issues) > SoftwareWriteIssueLimit {
		return nil, &ValidationError{Field: "issue", Message: fmt.Sprintf("at most %d issues are allowed", SoftwareWriteIssueLimit)}
	}
	clean := make([]string, len(issues))
	for i, issue := range issues {
		clean[i] = strings.TrimSpace(issue)
		if clean[i] == "" {
			return nil, &ValidationError{Field: "issue", Message: fmt.Sprintf("item %d must not be empty", i)}
		}
	}
	return clean, nil
}

func normalizeSoftwareOffsetPage(startAt, maxResults int, total *int, isLast bool, returned int) DiscoveryPage {
	hasMore := !isLast
	if total != nil {
		hasMore = startAt+returned < *total
	}
	next := 0
	if hasMore {
		next = startAt + returned
	}
	return DiscoveryPage{
		StartAt: startAt, MaxResults: maxResults, Returned: returned,
		Total: total, Next: next, HasMore: hasMore,
	}
}

func normalizeSoftwareIssuePage(deployment Deployment, raw softwareIssueResponse) (SearchPage, error) {
	page := SearchPage{Returned: len(raw.Issues), Limit: raw.MaxResults, Pages: 1, Total: raw.Total}
	if deployment == DeploymentCloud {
		page.Next = raw.NextPageToken
		page.HasMore = raw.NextPageToken != ""
		if raw.IsLast != nil && !*raw.IsLast && raw.NextPageToken == "" {
			return SearchPage{}, errors.New("Jira Software returned a non-final page without nextPageToken")
		}
		page.Total = nil
		return page, nil
	}
	start := raw.StartAt
	if raw.Total != nil {
		page.HasMore = start+len(raw.Issues) < *raw.Total
	} else if raw.IsLast != nil {
		page.HasMore = !*raw.IsLast
	}
	if page.HasMore {
		page.Next = strconv.Itoa(start + len(raw.Issues))
	}
	return page, nil
}

func parseSoftwareCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(cursor)
	if err != nil || value < 0 {
		return 0, &ValidationError{Field: "cursor", Message: "Data Center cursor must be a non-negative integer"}
	}
	return value, nil
}

func validateSoftwarePage(startAt, maxResults int) error {
	if startAt < 0 {
		return &ValidationError{Field: "start", Message: "must be non-negative"}
	}
	if maxResults < 1 || maxResults > SoftwareReadPageLimit {
		return &ValidationError{Field: "max", Message: fmt.Sprintf("must be between 1 and %d", SoftwareReadPageLimit)}
	}
	return nil
}

func validateSprintStates(state string) error {
	if state == "" {
		return nil
	}
	for _, value := range strings.Split(state, ",") {
		switch strings.TrimSpace(value) {
		case "active", "future", "closed":
		default:
			return &ValidationError{Field: "state", Message: "must contain only active, future, or closed"}
		}
	}
	return nil
}
