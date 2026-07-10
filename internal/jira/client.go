package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
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

func NewClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Myself(ctx context.Context) (*User, error) {
	var result User
	err := c.do(ctx, http.MethodGet, "/rest/api/2/myself", nil, &result)
	return &result, err
}

func (c *Client) Search(ctx context.Context, jql, fields string, maxResults int) (*SearchResponse, error) {
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("fields", fields)
	q.Set("maxResults", strconv.Itoa(maxResults))

	var result SearchResponse
	err := c.do(ctx, http.MethodGet, "/rest/api/2/search?"+q.Encode(), nil, &result)
	return &result, err
}

func (c *Client) GetIssue(ctx context.Context, key, fields string) (*Issue, error) {
	q := url.Values{}
	q.Set("fields", fields)

	var result Issue
	err := c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"?"+q.Encode(), nil, &result)
	return &result, err
}

func (c *Client) GetIssueRaw(ctx context.Context, key string) (*RawIssue, error) {
	var result RawIssue
	err := c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key), nil, &result)
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

	var result CreatedIssue
	err := c.do(ctx, http.MethodPost, "/rest/api/2/issue", map[string]any{"fields": fields}, &result)
	return &result, err
}

func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	return c.do(ctx, http.MethodPut, "/rest/api/2/issue/"+url.PathEscape(key), map[string]any{"fields": fields}, nil)
}

func (c *Client) AddComment(ctx context.Context, key, comment string) (*Comment, error) {
	var result Comment
	err := c.do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", map[string]any{"body": comment}, &result)
	return &result, err
}

func (c *Client) Transitions(ctx context.Context, key string) (*TransitionResponse, error) {
	var result TransitionResponse
	err := c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, &result)
	return &result, err
}

func (c *Client) TransitionIssue(ctx context.Context, key, transitionID string) error {
	body := map[string]any{
		"transition": map[string]string{
			"id": transitionID,
		},
	}
	return c.do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", body, nil)
}

func (c *Client) Fields(ctx context.Context) ([]Field, error) {
	var result []Field
	err := c.do(ctx, http.MethodGet, "/rest/api/2/field", nil, &result)
	return result, err
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
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

	req.Header.Set("Authorization", "Bearer "+c.token)
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
