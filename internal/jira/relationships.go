package jira

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) IssueLinkTypes(ctx context.Context) ([]IssueLinkType, error) {
	path, err := c.PlatformPath(ctx, 3, "/issueLinkType")
	if err != nil {
		return nil, err
	}
	var result struct {
		Types []IssueLinkType `json:"issueLinkTypes"`
	}
	if err := c.doRead(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result.Types, nil
}

func (c *Client) IssueLinks(ctx context.Context, key string) ([]IssueLinkView, error) {
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	var result struct {
		Fields struct {
			Links []IssueLink `json:"issuelinks"`
		} `json:"fields"`
	}
	if err := c.doRead(ctx, http.MethodGet, path+"?fields=issuelinks", nil, &result); err != nil {
		return nil, err
	}
	links := make([]IssueLinkView, 0, len(result.Fields.Links))
	for _, link := range result.Fields.Links {
		view := IssueLinkView{ID: link.ID, Type: link.Type}
		switch {
		case link.OutwardIssue != nil:
			view.Direction = "outward"
			view.Relation = link.Type.Outward
			view.Issue = *link.OutwardIssue
		case link.InwardIssue != nil:
			view.Direction = "inward"
			view.Relation = link.Type.Inward
			view.Issue = *link.InwardIssue
		default:
			continue
		}
		links = append(links, view)
	}
	return links, nil
}

func (c *Client) AddIssueLink(ctx context.Context, linkType, outwardIssue, inwardIssue string) (*LinkReceipt, error) {
	path, err := c.PlatformPath(ctx, 3, "/issueLink")
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"type":         map[string]string{"name": linkType},
		"outwardIssue": map[string]string{"key": outwardIssue},
		"inwardIssue":  map[string]string{"key": inwardIssue},
	}
	if err := c.do(ctx, http.MethodPost, path, payload, nil); err != nil {
		return nil, err
	}
	return &LinkReceipt{
		Accepted:                   true,
		DuplicateRequestsSucceed:   true,
		ServerReturnsCreatedLinkID: false,
		OutwardIssue:               outwardIssue,
		InwardIssue:                inwardIssue,
		Type:                       linkType,
	}, nil
}

func (c *Client) RemoveIssueLink(ctx context.Context, linkID string) error {
	path, err := c.PlatformPath(ctx, 3, "/issueLink/"+url.PathEscape(linkID))
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
