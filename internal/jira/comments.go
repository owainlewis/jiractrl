package jira

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

func (c *Client) Comments(ctx context.Context, key string, startAt, maxResults int) (*CommentPage, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(key)+"/comment")
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"startAt":    {strconv.Itoa(startAt)},
		"maxResults": {strconv.Itoa(maxResults)},
	}
	var raw struct {
		StartAt    int       `json:"startAt"`
		MaxResults int       `json:"maxResults"`
		Total      int       `json:"total"`
		Comments   []Comment `json:"comments"`
	}
	if err := c.doRead(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	return &CommentPage{
		Comments: raw.Comments,
		Page: pageFromOffset(raw.StartAt, raw.MaxResults, len(raw.Comments), raw.Total,
			raw.StartAt+len(raw.Comments) >= raw.Total),
	}, nil
}

func (c *Client) AddCommentWithPayload(ctx context.Context, key string, payload map[string]any) (*Comment, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(key)+"/comment")
	if err != nil {
		return nil, err
	}
	var result Comment
	err = c.do(ctx, http.MethodPost, path, payload, &result)
	return &result, err
}

func (c *Client) UpdateCommentWithPayload(ctx context.Context, key, commentID string, payload map[string]any) (*Comment, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(commentID))
	if err != nil {
		return nil, err
	}
	var result Comment
	err = c.do(ctx, http.MethodPut, path, payload, &result)
	return &result, err
}

func (c *Client) RemoveComment(ctx context.Context, key, commentID string) error {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(commentID))
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
