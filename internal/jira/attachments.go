package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

func (c *Client) IssueAttachments(ctx context.Context, key string) ([]Attachment, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var result struct {
		Fields struct {
			Attachments []Attachment `json:"attachment"`
		} `json:"fields"`
	}
	if err := c.doRead(ctx, http.MethodGet, path+"?fields=attachment", nil, &result); err != nil {
		return nil, err
	}
	return result.Fields.Attachments, nil
}

func (c *Client) Attachment(ctx context.Context, id string) (*Attachment, error) {
	path, err := c.PlatformPath(ctx, 3, "/attachment/"+url.PathEscape(id))
	if err != nil {
		return nil, err
	}
	var result Attachment
	if err := c.doRead(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) UploadAttachment(ctx context.Context, issue, filename string, source io.Reader) ([]Attachment, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/attachments")
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	contentType := form.FormDataContentType()
	go func() {
		var writeErr error
		part, writeErr := form.CreateFormFile("file", filepath.Base(filename))
		if writeErr == nil {
			_, writeErr = io.Copy(part, source)
		}
		if closeErr := form.Close(); writeErr == nil {
			writeErr = closeErr
		}
		_ = writer.CloseWithError(writeErr)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		return nil, err
	}
	c.applyAuth(req, c.authMode())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Atlassian-Token", "no-check")

	resp, err := c.http.Do(req)
	if err != nil {
		_ = reader.CloseWithError(err)
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.responseError(resp, body)
	}
	var result []Attachment
	if err := json.Unmarshal(body, &result); err != nil {
		var single Attachment
		if singleErr := json.Unmarshal(body, &single); singleErr != nil {
			return nil, err
		}
		result = []Attachment{single}
	}
	return result, nil
}

func (c *Client) DownloadAttachment(ctx context.Context, id string, destination io.Writer) (int64, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return 0, err
	}
	target := ""
	if deployment == DeploymentCloud {
		path, err := c.PlatformPath(ctx, 3, "/attachment/content/"+url.PathEscape(id))
		if err != nil {
			return 0, err
		}
		target = c.baseURL + path + "?redirect=false"
	} else {
		attachment, err := c.Attachment(ctx, id)
		if err != nil {
			return 0, err
		}
		target, err = c.sameOriginURL(attachment.Content)
		if err != nil {
			return 0, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	c.applyAuth(req, c.authMode())
	req.Header.Set("Accept", "application/octet-stream")
	httpClient := *c.http
	previousRedirectPolicy := c.http.CheckRedirect
	httpClient.CheckRedirect = func(redirected *http.Request, via []*http.Request) error {
		if !c.isSameOrigin(redirected.URL) {
			return errors.New("attachment download redirect target is not on the configured Jira origin")
		}
		if previousRedirectPolicy != nil {
			return previousRedirectPolicy(redirected, via)
		}
		return nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return 0, readErr
		}
		return 0, c.responseError(resp, body)
	}
	return io.Copy(destination, resp.Body)
}

func (c *Client) sameOriginURL(value string) (string, error) {
	if value == "" {
		return "", errors.New("Jira attachment metadata did not include a content URL")
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	target, err := base.Parse(value)
	if err != nil {
		return "", err
	}
	if !c.isSameOrigin(target) {
		return "", errors.New("Jira attachment content URL is not on the configured Jira origin")
	}
	return target.String(), nil
}

func (c *Client) isSameOrigin(target *url.URL) bool {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(target.Scheme, base.Scheme) &&
		strings.EqualFold(target.Hostname(), base.Hostname()) &&
		target.Port() == base.Port()
}

func (c *Client) RemoveAttachment(ctx context.Context, id string) error {
	path, err := c.PlatformPath(ctx, 3, "/attachment/"+url.PathEscape(id))
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
