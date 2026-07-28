package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	http               *http.Client
}

type Error struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *Error) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("jira request failed: %s", e.Status)
	}
	return fmt.Sprintf("jira request failed: %s: %s", e.Status, e.Body)
}

func (e *Error) IsAuth() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

type UnsupportedCapabilityError struct {
	Capability string
	Deployment Deployment
}

func (e *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("Jira capability %q is unavailable for deployment %q", e.Capability, e.Deployment)
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
	}
}

func (c *Client) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	info, err := c.fetchServerInfo(ctx)
	if err != nil {
		if c.deploymentOverride == "" || c.deploymentOverride == DeploymentAuto {
			return nil, err
		}
		return &ServerInfo{
			BaseURL:          c.baseURL,
			Deployment:       c.deploymentOverride,
			DeploymentSource: "config",
			Capabilities:     capabilitiesFor(c.deploymentOverride),
		}, nil
	}

	deployment, source, err := c.resolveDeployment(info)
	if err != nil {
		return nil, err
	}
	info.Deployment = deployment
	info.DeploymentSource = source
	info.Capabilities = capabilitiesFor(deployment)
	return info, nil
}

func (c *Client) Deployment(ctx context.Context) (Deployment, error) {
	c.detectOnce.Do(func() {
		if c.deploymentOverride != "" && c.deploymentOverride != DeploymentAuto {
			c.deployment = c.deploymentOverride
			return
		}
		info, err := c.fetchServerInfo(ctx)
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
	err = c.do(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) Search(ctx context.Context, jql, fields string, maxResults int) (*SearchResponse, error) {
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("fields", fields)
	q.Set("maxResults", strconv.Itoa(maxResults))

	path, err := c.PlatformPath(ctx, 2, "/search")
	if err != nil {
		return nil, err
	}
	var result SearchResponse
	err = c.do(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &result)
	return &result, err
}

func (c *Client) GetIssue(ctx context.Context, key, fields string) (*Issue, error) {
	q := url.Values{}
	q.Set("fields", fields)

	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var result Issue
	err = c.do(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &result)
	return &result, err
}

func (c *Client) GetIssueRaw(ctx context.Context, key string) (*RawIssue, error) {
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var result RawIssue
	err = c.do(ctx, http.MethodGet, path, nil, &result)
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

	path, err := c.PlatformPath(ctx, 2, "/issue")
	if err != nil {
		return nil, err
	}
	var result CreatedIssue
	err = c.do(ctx, http.MethodPost, path, map[string]any{"fields": fields}, &result)
	return &result, err
}

func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key))
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPut, path, map[string]any{"fields": fields}, nil)
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
	err = c.do(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) TransitionIssue(ctx context.Context, key, transitionID string) error {
	body := map[string]any{
		"transition": map[string]string{
			"id": transitionID,
		},
	}
	path, err := c.PlatformPath(ctx, 2, "/issue/"+url.PathEscape(key)+"/transitions")
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, body, nil)
}

func (c *Client) Fields(ctx context.Context) ([]Field, error) {
	path, err := c.PlatformPath(ctx, 2, "/field")
	if err != nil {
		return nil, err
	}
	var result []Field
	err = c.do(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

func (c *Client) fetchServerInfo(ctx context.Context) (*ServerInfo, error) {
	var result ServerInfo
	err := c.do(ctx, http.MethodGet, "/rest/api/2/serverInfo", nil, &result)
	var jiraErr *Error
	if err != nil &&
		(c.deploymentOverride == "" || c.deploymentOverride == DeploymentAuto) &&
		c.email != "" &&
		errors.As(err, &jiraErr) &&
		jiraErr.IsAuth() {
		result = ServerInfo{}
		err = c.doWithAuth(ctx, http.MethodGet, "/rest/api/2/serverInfo", nil, &result, authBasic)
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

	if mode == authBasic {
		req.SetBasicAuth(c.email, c.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
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
		return &Error{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       strings.TrimSpace(string(respBody)),
		}
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}
