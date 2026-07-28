package jira

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

func (c *Client) Worklogs(ctx context.Context, issue string, startAt, maxResults int) (*WorklogPage, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/worklog")
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"startAt":    {strconv.Itoa(startAt)},
		"maxResults": {strconv.Itoa(maxResults)},
	}
	var raw struct {
		StartAt    int       `json:"startAt"`
		MaxResults int       `json:"maxResults"`
		Total      int       `json:"total"`
		Worklogs   []Worklog `json:"worklogs"`
	}
	if err := c.doRead(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	return &WorklogPage{
		Worklogs: raw.Worklogs,
		Page: pageFromOffset(raw.StartAt, raw.MaxResults, len(raw.Worklogs), raw.Total,
			raw.StartAt+len(raw.Worklogs) >= raw.Total),
	}, nil
}

func (c *Client) AddWorklog(ctx context.Context, issue string, payload map[string]any, query url.Values) (*Worklog, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/worklog")
	if err != nil {
		return nil, err
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var result Worklog
	if err := c.do(ctx, http.MethodPost, path, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) UpdateWorklog(ctx context.Context, issue, worklogID string, payload map[string]any, query url.Values) (*Worklog, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/worklog/"+url.PathEscape(worklogID))
	if err != nil {
		return nil, err
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var result Worklog
	if err := c.do(ctx, http.MethodPut, path, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Watchers(ctx context.Context, issue string) (*Watchers, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/watchers")
	if err != nil {
		return nil, err
	}
	var result Watchers
	if err := c.doRead(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) AddWatcher(ctx context.Context, issue, identity string, self bool) (*WatcherReceipt, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/watchers")
	if err != nil {
		return nil, err
	}
	if self && deployment == DeploymentDataCenter {
		current, err := c.Myself(ctx)
		if err != nil {
			return nil, err
		}
		identity = current.Name
		if identity == "" {
			return nil, &ValidationError{Field: "user", Message: "Jira Data Center did not return the current username"}
		}
	}
	var body any
	if !self || deployment == DeploymentDataCenter {
		body = identity
	}
	if err := c.do(ctx, http.MethodPost, path, body, nil); err != nil {
		return nil, err
	}
	receipt := &WatcherReceipt{Issue: issue, Deployment: deployment, Self: self, Watching: true}
	if deployment == DeploymentCloud {
		receipt.AccountID = identity
	} else {
		receipt.User = identity
	}
	return receipt, nil
}

func (c *Client) RemoveWatcher(ctx context.Context, issue, identity string, self bool) (*WatcherReceipt, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	if self {
		current, err := c.Myself(ctx)
		if err != nil {
			return nil, err
		}
		if deployment == DeploymentCloud {
			identity = current.AccountID
			if identity == "" {
				return nil, &ValidationError{Field: "account-id", Message: "Jira Cloud did not return the current account ID"}
			}
		} else {
			identity = current.Name
			if identity == "" {
				return nil, &ValidationError{Field: "user", Message: "Jira Data Center did not return the current username"}
			}
		}
	}
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/watchers")
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if deployment == DeploymentCloud {
		query.Set("accountId", identity)
	} else {
		query.Set("username", identity)
	}
	path += "?" + query.Encode()
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return nil, err
	}
	receipt := &WatcherReceipt{Issue: issue, Deployment: deployment, Self: self, Watching: false}
	if deployment == DeploymentCloud {
		receipt.AccountID = identity
	} else {
		receipt.User = identity
	}
	return receipt, nil
}
