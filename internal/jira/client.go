package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL            string
	token              string
	email              string
	deploymentOverride Deployment
	deployment         Deployment
	deploymentErr      error
	detectOnce         sync.Once
	softwareOnce       sync.Once
	softwareStatus     CapabilityStatus
	softwareErr        error
	http               *http.Client
	retry              RetryPolicy
	sleep              func(context.Context, time.Duration) error
	jitter             func(time.Duration) time.Duration
}

type Error struct {
	StatusCode      int
	Status          string
	Body            string
	ErrorMessages   []string
	FieldErrors     map[string]string
	Attempts        int
	RetryAfter      time.Duration
	RetryAfterSet   bool
	RateLimitReason string
	RateLimitLimit  string
	RateLimitRemain string
	RateLimitReset  string
	RateLimitNear   string
}

func (e *Error) Error() string {
	if len(e.ErrorMessages) > 0 {
		return fmt.Sprintf("jira request failed: %s: %s", e.Status, strings.Join(e.ErrorMessages, "; "))
	}
	return fmt.Sprintf("jira request failed: %s", e.Status)
}

func (e *Error) IsAuth() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

type UnsupportedCapabilityError struct {
	Capability string
	Deployment Deployment
}

func (e *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("Jira capability %q is unavailable for deployment %q", e.Capability, e.Deployment)
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Message)
}

func ParseDeployment(value string) (Deployment, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return DeploymentAuto, nil
	case "cloud":
		return DeploymentCloud, nil
	case "data_center", "datacenter", "server":
		return DeploymentDataCenter, nil
	default:
		return "", fmt.Errorf("invalid Jira deployment %q: use auto, cloud, or data_center", value)
	}
}

func NewClient(baseURL, token, email string, deployment Deployment, timeout time.Duration) *Client {
	return &Client{
		baseURL:            strings.TrimRight(baseURL, "/"),
		token:              token,
		email:              email,
		deploymentOverride: deployment,
		http: &http.Client{
			Timeout: timeout,
		},
		retry: RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   500 * time.Millisecond,
			MaxDelay:    30 * time.Second,
		},
		sleep: sleepContext,
		jitter: func(delay time.Duration) time.Duration {
			if delay <= 1 {
				return delay
			}
			return delay/2 + time.Duration(rand.Int63n(int64(delay-delay/2)))
		},
	}
}

func (c *Client) SetRetryPolicy(policy RetryPolicy) {
	c.retry = policy
}

func (c *Client) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	info, err := c.fetchServerInfo(ctx)
	if err != nil {
		if c.deploymentOverride == "" || c.deploymentOverride == DeploymentAuto {
			return nil, err
		}
		info = &ServerInfo{
			BaseURL:          c.baseURL,
			Deployment:       c.deploymentOverride,
			DeploymentSource: "config",
			Capabilities:     capabilitiesFor(c.deploymentOverride),
		}
	} else {
		deployment, source, err := c.resolveDeployment(info)
		if err != nil {
			return nil, err
		}
		info.Deployment = deployment
		info.DeploymentSource = source
		info.Capabilities = capabilitiesFor(deployment)
	}

	status, _ := c.softwareCapability(ctx, info.Deployment)
	info.Capabilities.Software = status
	return info, nil
}

func (c *Client) SoftwareCapability(ctx context.Context) (CapabilityStatus, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return CapabilityUnknown, err
	}
	return c.softwareCapability(ctx, deployment)
}

func (c *Client) softwareCapability(ctx context.Context, deployment Deployment) (CapabilityStatus, error) {
	c.softwareOnce.Do(func() {
		var result struct {
			Values []json.RawMessage `json:"values"`
		}
		mode := authBearer
		if deployment == DeploymentCloud && c.email != "" {
			mode = authBasic
		}
		err := c.doReadWithAuth(ctx, http.MethodGet, "/rest/agile/1.0/board?maxResults=1", nil, &result, mode)
		if err == nil {
			c.softwareStatus = CapabilityAvailable
			return
		}
		var jiraErr *Error
		if errors.As(err, &jiraErr) && jiraErr.StatusCode == http.StatusNotFound {
			c.softwareStatus = CapabilityMissing
			return
		}
		c.softwareStatus = CapabilityUnknown
		c.softwareErr = err
	})
	return c.softwareStatus, c.softwareErr
}

func (c *Client) RequireSoftware(ctx context.Context) error {
	status, err := c.SoftwareCapability(ctx)
	if err != nil {
		return err
	}
	return c.RequireCapability(ctx, "jira_software", status)
}

func (c *Client) Deployment(ctx context.Context) (Deployment, error) {
	return c.detectDeployment(ctx, c.fetchServerInfo)
}

func (c *Client) detectDeployment(ctx context.Context, fetch func(context.Context) (*ServerInfo, error)) (Deployment, error) {
	c.detectOnce.Do(func() {
		if c.deploymentOverride != "" && c.deploymentOverride != DeploymentAuto {
			c.deployment = c.deploymentOverride
			return
		}
		info, err := fetch(ctx)
		if err != nil {
			c.deploymentErr = fmt.Errorf("detect Jira deployment: %w; set jira.deployment to cloud or data_center to override detection", err)
			return
		}
		c.deployment, _, c.deploymentErr = c.resolveDeployment(info)
	})
	return c.deployment, c.deploymentErr
}

func (c *Client) PlatformPath(ctx context.Context, cloudVersion int, resource string) (string, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return "", err
	}
	version := 2
	if deployment == DeploymentCloud {
		version = cloudVersion
	}
	resource = "/" + strings.TrimLeft(resource, "/")
	return fmt.Sprintf("/rest/api/%d%s", version, resource), nil
}

func (c *Client) RequireCapability(ctx context.Context, capability string, status CapabilityStatus) error {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return err
	}
	if status == CapabilityMissing {
		return &UnsupportedCapabilityError{Capability: capability, Deployment: deployment}
	}
	return nil
}

func (c *Client) Myself(ctx context.Context) (*User, error) {
	var result User
	path, err := c.PlatformPath(ctx, 2, "/myself")
	if err != nil {
		return nil, err
	}
	err = c.doRead(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) Search(ctx context.Context, options SearchOptions) (*SearchResponse, error) {
	if err := validateSearchOptions(options); err != nil {
		return nil, err
	}

	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	if deployment == DeploymentCloud {
		return c.searchCloud(ctx, options)
	}
	return c.searchDataCenter(ctx, options)
}

func (c *Client) SearchRawJSON(ctx context.Context, options SearchOptions) (json.RawMessage, error) {
	if err := validateSearchOptions(options); err != nil {
		return nil, err
	}
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	path := ""
	body := map[string]any{
		"jql":        options.JQL,
		"fields":     options.Fields,
		"maxResults": options.MaxResults,
	}
	if deployment == DeploymentCloud {
		path, err = c.PlatformPath(ctx, 3, "/search/jql")
		if options.Cursor != "" {
			body["nextPageToken"] = options.Cursor
		}
		if len(options.ReconcileIssueIDs) > 0 {
			body["reconcileIssues"] = options.ReconcileIssueIDs
		}
	} else {
		if len(options.ReconcileIssueIDs) > 0 {
			return nil, &UnsupportedCapabilityError{
				Capability: "search reconciliation",
				Deployment: DeploymentDataCenter,
			}
		}
		startAt := 0
		if options.Cursor != "" {
			startAt, err = strconv.Atoi(options.Cursor)
			if err != nil || startAt < 0 {
				return nil, &ValidationError{Field: "cursor", Message: "must be a non-negative Data Center offset"}
			}
		}
		path, err = c.PlatformPath(ctx, 2, "/search")
		body["startAt"] = startAt
	}
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.doRead(ctx, http.MethodPost, path, body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func validateSearchOptions(options SearchOptions) error {
	if strings.TrimSpace(options.JQL) == "" {
		return errors.New("search JQL must not be empty")
	}
	if options.MaxResults < 1 || options.MaxResults > 1000 {
		return &ValidationError{Field: "maxResults", Message: "must be between 1 and 1000"}
	}
	if len(options.ReconcileIssueIDs) > 50 {
		return &ValidationError{Field: "reconcileIssues", Message: "must contain at most 50 issue IDs"}
	}
	for _, issueID := range options.ReconcileIssueIDs {
		if issueID < 1 {
			return &ValidationError{Field: "reconcileIssues", Message: "issue IDs must be positive integers"}
		}
	}
	return nil
}

func (c *Client) SearchAll(ctx context.Context, options SearchOptions, limit int) (*SearchResponse, error) {
	if limit < 1 {
		return nil, errors.New("search limit must be 1 or greater")
	}
	if options.MaxResults < 1 || options.MaxResults > 1000 {
		return nil, &ValidationError{Field: "maxResults", Message: "must be between 1 and 1000"}
	}

	const maxPages = 1000
	pageSize := options.MaxResults
	cursor := options.Cursor
	seen := map[string]bool{}
	if cursor != "" {
		seen[cursor] = true
	}
	result := &SearchResponse{
		Issues: make([]Issue, 0, min(pageSize, limit)),
		Page: SearchPage{
			Limit: limit,
		},
	}

	for result.Page.Pages < maxPages && len(result.Issues) < limit {
		pageOptions := options
		pageOptions.Cursor = cursor
		pageOptions.MaxResults = min(pageSize, limit-len(result.Issues))
		page, err := c.Search(ctx, pageOptions)
		if err != nil {
			return nil, err
		}
		if len(page.Issues) > pageOptions.MaxResults {
			return nil, fmt.Errorf("Jira search returned %d issues for a page size of %d", len(page.Issues), pageOptions.MaxResults)
		}

		result.Issues = append(result.Issues, page.Issues...)
		result.Page.Pages++
		result.Page.Total = page.Page.Total
		result.Page.Next = page.Page.Next
		result.Page.HasMore = page.Page.HasMore

		if !page.Page.HasMore || len(result.Issues) >= limit {
			break
		}
		if page.Page.Next == "" {
			return nil, errors.New("Jira search reported another page without a continuation cursor")
		}
		if seen[page.Page.Next] {
			return nil, fmt.Errorf("Jira search repeated continuation cursor %q", page.Page.Next)
		}
		seen[page.Page.Next] = true
		cursor = page.Page.Next
	}
	if result.Page.Pages == maxPages && result.Page.HasMore && len(result.Issues) < limit {
		return nil, fmt.Errorf("Jira search exceeded the %d-page safety budget", maxPages)
	}
	result.Page.Returned = len(result.Issues)
	return result, nil
}

func (c *Client) searchCloud(ctx context.Context, options SearchOptions) (*SearchResponse, error) {
	path, err := c.PlatformPath(ctx, 3, "/search/jql")
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"jql":        options.JQL,
		"fields":     options.Fields,
		"maxResults": options.MaxResults,
	}
	if options.Cursor != "" {
		body["nextPageToken"] = options.Cursor
	}
	if len(options.ReconcileIssueIDs) > 0 {
		body["reconcileIssues"] = options.ReconcileIssueIDs
	}

	var raw struct {
		Issues        []Issue `json:"issues"`
		NextPageToken string  `json:"nextPageToken"`
		IsLast        bool    `json:"isLast"`
	}
	if err := c.doRead(ctx, http.MethodPost, path, body, &raw); err != nil {
		return nil, err
	}
	hasMore := raw.NextPageToken != "" && !raw.IsLast
	return &SearchResponse{
		Issues: raw.Issues,
		Page: SearchPage{
			Returned: len(raw.Issues),
			Limit:    options.MaxResults,
			Next:     raw.NextPageToken,
			HasMore:  hasMore,
			Pages:    1,
		},
	}, nil
}

func (c *Client) searchDataCenter(ctx context.Context, options SearchOptions) (*SearchResponse, error) {
	if len(options.ReconcileIssueIDs) > 0 {
		return nil, &UnsupportedCapabilityError{
			Capability: "search reconciliation",
			Deployment: DeploymentDataCenter,
		}
	}
	startAt := 0
	if options.Cursor != "" {
		parsed, err := strconv.Atoi(options.Cursor)
		if err != nil || parsed < 0 {
			return nil, &ValidationError{
				Field:   "cursor",
				Message: fmt.Sprintf("%q is not a non-negative Data Center offset", options.Cursor),
			}
		}
		startAt = parsed
	}

	path, err := c.PlatformPath(ctx, 2, "/search")
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"jql":        options.JQL,
		"fields":     options.Fields,
		"maxResults": options.MaxResults,
		"startAt":    startAt,
	}
	var raw struct {
		StartAt    int     `json:"startAt"`
		MaxResults int     `json:"maxResults"`
		Total      int     `json:"total"`
		Issues     []Issue `json:"issues"`
	}
	if err := c.doRead(ctx, http.MethodPost, path, body, &raw); err != nil {
		return nil, err
	}

	nextAt := raw.StartAt + len(raw.Issues)
	hasMore := nextAt < raw.Total
	next := ""
	if hasMore {
		next = strconv.Itoa(nextAt)
	}
	total := raw.Total
	return &SearchResponse{
		Issues: raw.Issues,
		Page: SearchPage{
			Returned: len(raw.Issues),
			Limit:    options.MaxResults,
			Next:     next,
			HasMore:  hasMore,
			Total:    &total,
			Pages:    1,
		},
	}, nil
}

func (c *Client) GetIssue(ctx context.Context, key, fields string) (*Issue, error) {
	q := url.Values{}
	q.Set("fields", fields)

	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var result Issue
	err = c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &result)
	return &result, err
}

func (c *Client) GetIssueRawJSON(ctx context.Context, key, fields string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("fields", fields)
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *Client) GetIssueRaw(ctx context.Context, key string) (*RawIssue, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var result RawIssue
	err = c.doRead(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) CreateIssue(ctx context.Context, project, issueType, summary, description string) (*CreatedIssue, error) {
	fields := map[string]any{
		"project": map[string]string{
			"key": project,
		},
		"issuetype": map[string]string{
			"name": issueType,
		},
		"summary": summary,
	}
	if strings.TrimSpace(description) != "" {
		fields["description"] = description
	}

	return c.CreateIssueWithPayload(ctx, map[string]any{"fields": fields})
}

func (c *Client) PlanCreateIssue(ctx context.Context, payload map[string]any) (*MutationRequest, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue")
	if err != nil {
		return nil, err
	}
	return &MutationRequest{Method: http.MethodPost, Path: path, Body: payload}, nil
}

func (c *Client) CreateIssueWithPayload(ctx context.Context, payload map[string]any) (*CreatedIssue, error) {
	request, err := c.PlanCreateIssue(ctx, payload)
	if err != nil {
		return nil, err
	}
	var result CreatedIssue
	err = c.do(ctx, request.Method, request.Path, request.Body, &result)
	return &result, err
}

func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	return c.UpdateIssueWithPayload(ctx, key, map[string]any{"fields": fields})
}

func (c *Client) PlanUpdateIssue(ctx context.Context, key string, payload map[string]any) (*MutationRequest, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	return &MutationRequest{Method: http.MethodPut, Path: path, Body: payload}, nil
}

func (c *Client) UpdateIssueWithPayload(ctx context.Context, key string, payload map[string]any) error {
	request, err := c.PlanUpdateIssue(ctx, key, payload)
	if err != nil {
		return err
	}
	return c.do(ctx, request.Method, request.Path, request.Body, nil)
}

func (c *Client) AddComment(ctx context.Context, key, comment string) (*Comment, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key)+"/comment")
	if err != nil {
		return nil, err
	}
	var result Comment
	err = c.do(ctx, http.MethodPost, path, map[string]any{"body": comment}, &result)
	return &result, err
}

func (c *Client) Transitions(ctx context.Context, key string) (*TransitionResponse, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key)+"/transitions")
	if err != nil {
		return nil, err
	}
	var result TransitionResponse
	err = c.doRead(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) TransitionIssue(ctx context.Context, key, transitionID string) error {
	return c.TransitionIssueWithPayload(ctx, key, map[string]any{
		"transition": map[string]string{
			"id": transitionID,
		},
	})
}

func (c *Client) PlanTransitionIssue(ctx context.Context, key string, payload map[string]any) (*MutationRequest, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key)+"/transitions")
	if err != nil {
		return nil, err
	}
	return &MutationRequest{Method: http.MethodPost, Path: path, Body: payload}, nil
}

func (c *Client) TransitionIssueWithPayload(ctx context.Context, key string, payload map[string]any) error {
	request, err := c.PlanTransitionIssue(ctx, key, payload)
	if err != nil {
		return err
	}
	return c.do(ctx, request.Method, request.Path, request.Body, nil)
}

func (c *Client) Fields(ctx context.Context) ([]Field, error) {
	path, err := c.PlatformPath(ctx, 2, "/field")
	if err != nil {
		return nil, err
	}
	var result []Field
	err = c.doRead(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

func (c *Client) fetchServerInfo(ctx context.Context) (*ServerInfo, error) {
	var result ServerInfo
	err := c.doRead(ctx, http.MethodGet, "/rest/api/2/serverInfo", nil, &result)
	var jiraErr *Error
	if err != nil &&
		(c.deploymentOverride == "" || c.deploymentOverride == DeploymentAuto) &&
		c.email != "" &&
		errors.As(err, &jiraErr) &&
		jiraErr.IsAuth() {
		result = ServerInfo{}
		err = c.doReadWithAuth(ctx, http.MethodGet, "/rest/api/2/serverInfo", nil, &result, authBasic)
	}
	return &result, err
}

func (c *Client) resolveDeployment(info *ServerInfo) (Deployment, string, error) {
	if c.deploymentOverride != "" && c.deploymentOverride != DeploymentAuto {
		return c.deploymentOverride, "config", nil
	}
	switch strings.ToLower(strings.TrimSpace(info.DeploymentType)) {
	case "cloud":
		return DeploymentCloud, "detected", nil
	case "server", "data center", "data_center", "datacenter":
		return DeploymentDataCenter, "detected", nil
	default:
		return "", "", fmt.Errorf("unrecognized Jira deployment type %q; set jira.deployment to cloud or data_center", info.DeploymentType)
	}
}

func capabilitiesFor(deployment Deployment) Capabilities {
	return Capabilities{
		Platform:          CapabilityAvailable,
		Software:          CapabilityUnknown,
		ServiceManagement: CapabilityUnknown,
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	return c.doWithAuth(ctx, method, path, body, out, c.authMode())
}

func (c *Client) doRaw(ctx context.Context, method, path string, body any) (int, json.RawMessage, error) {
	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		requestBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return 0, nil, err
	}
	c.applyAuth(req, c.authMode())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, nil, c.responseError(resp, responseBody)
	}
	return resp.StatusCode, json.RawMessage(responseBody), nil
}

func (c *Client) doRead(ctx context.Context, method, path string, body any, out any) error {
	return c.doReadWithAuth(ctx, method, path, body, out, c.authMode())
}

type authMode int

const (
	authBearer authMode = iota
	authBasic
)

func (c *Client) authMode() authMode {
	deployment := c.deployment
	if deployment == "" || deployment == DeploymentAuto {
		deployment = c.deploymentOverride
	}
	if deployment == DeploymentCloud && c.email != "" {
		return authBasic
	}
	return authBearer
}

func (c *Client) doWithAuth(ctx context.Context, method, path string, body any, out any, mode authMode) error {
	return c.doAttempt(ctx, method, path, body, out, mode)
}

func (c *Client) doReadWithAuth(ctx context.Context, method, path string, body any, out any, mode authMode) error {
	attempts := c.retry.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		err := c.doAttempt(ctx, method, path, body, out, mode)
		if err == nil {
			return nil
		}

		var jiraErr *Error
		retryable := errors.As(err, &jiraErr) &&
			(jiraErr.StatusCode == http.StatusTooManyRequests || retryableServerStatus(jiraErr.StatusCode))
		if jiraErr != nil {
			jiraErr.Attempts = attempt
		}
		if !retryable || attempt == attempts {
			return err
		}

		delay := c.retryDelay(attempt, jiraErr)
		if err := c.sleep(ctx, delay); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) retryDelay(attempt int, jiraErr *Error) time.Duration {
	delay := c.retry.BaseDelay
	for i := 1; i < attempt; i++ {
		if c.retry.MaxDelay > 0 && delay >= c.retry.MaxDelay/2 {
			delay = c.retry.MaxDelay
			break
		}
		delay *= 2
	}
	if c.jitter != nil {
		delay = c.jitter(delay)
	}
	if jiraErr != nil && jiraErr.RetryAfterSet && jiraErr.RetryAfter > delay {
		delay = jiraErr.RetryAfter
	}
	if delay > c.retry.MaxDelay {
		delay = c.retry.MaxDelay
	}
	return delay
}

func retryableServerStatus(status int) bool {
	switch status {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (c *Client) doAttempt(ctx context.Context, method, path string, body any, out any, mode authMode) error {
	var requestBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return err
	}

	c.applyAuth(req, mode)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.responseError(resp, respBody)
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if raw, ok := out.(*json.RawMessage); ok {
		*raw = append((*raw)[:0], respBody...)
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *Client) applyAuth(req *http.Request, mode authMode) {
	if mode == authBasic {
		req.SetBasicAuth(c.email, c.token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
}

func (c *Client) responseError(resp *http.Response, body []byte) error {
	retryAfterValue := firstNonEmptyHeader(resp.Header, "Retry-After", "Beta-Retry-After")
	result := &Error{
		StatusCode:    resp.StatusCode,
		Status:        resp.Status,
		Body:          strings.TrimSpace(string(body)),
		Attempts:      1,
		RetryAfter:    parseRetryAfter(retryAfterValue, time.Now()),
		RetryAfterSet: retryAfterValue != "",
		RateLimitReason: firstNonEmptyHeader(resp.Header,
			"RateLimit-Reason", "X-RateLimit-Reason"),
		RateLimitLimit: firstNonEmptyHeader(resp.Header,
			"X-RateLimit-Limit", "X-Beta-RateLimit-Limit"),
		RateLimitRemain: firstNonEmptyHeader(resp.Header,
			"X-RateLimit-Remaining", "X-Beta-RateLimit-Remaining"),
		RateLimitReset: firstNonEmptyHeader(resp.Header,
			"X-RateLimit-Reset", "X-Beta-RateLimit-Reset"),
		RateLimitNear: firstNonEmptyHeader(resp.Header,
			"X-RateLimit-NearLimit", "X-Beta-RateLimit-NearLimit"),
	}
	var details struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if json.Unmarshal(body, &details) == nil {
		result.ErrorMessages = c.redactStrings(details.ErrorMessages)
		result.FieldErrors = c.redactMap(details.Errors)
	}
	result.Body = c.redact(result.Body)
	return result
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return deadline.Sub(now)
	}
	return 0
}

func firstNonEmptyHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func (c *Client) redactStrings(values []string) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = c.redact(value)
	}
	return result
}

func (c *Client) redactMap(values map[string]string) map[string]string {
	if values == nil {
		return values
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = c.redact(value)
	}
	return result
}

func (c *Client) redact(value string) string {
	secrets := []string{c.token}
	if c.email != "" && c.token != "" {
		secrets = append(secrets,
			c.email+":"+c.token,
			base64.StdEncoding.EncodeToString([]byte(c.email+":"+c.token)),
		)
	}
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}
