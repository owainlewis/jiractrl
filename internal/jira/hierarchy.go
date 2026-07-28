package jira

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) ResolveAssignableUser(ctx context.Context, issue, query string) (*UserIdentity, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &ValidationError{Field: "user", Message: "must not be empty"}
	}
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}

	const pageSize = 50
	var matches []UserIdentity
	seen := map[string]bool{}
	for startAt, pages := 0, 0; pages < 20; pages++ {
		page, err := c.AssignableUsers(ctx, "", issue, query, startAt, pageSize)
		if err != nil {
			return nil, err
		}
		for _, user := range page.Users {
			if !user.Active || !userMatches(user, query, deployment) {
				continue
			}
			identity := userIdentityKey(user, deployment)
			if identity != "" && !seen[identity] {
				seen[identity] = true
				matches = append(matches, user)
			}
		}
		if !page.Page.HasMore {
			break
		}
		if pages == 19 {
			return nil, errors.New("assignable-user resolution exceeded the 1,000-user safety budget")
		}
		if page.Page.Next <= startAt {
			return nil, errors.New("Jira returned a non-advancing assignable-user page")
		}
		startAt = page.Page.Next
	}

	switch len(matches) {
	case 0:
		return nil, &ValidationError{Field: "user", Message: "no exact assignable user matched"}
	case 1:
		return &matches[0], nil
	default:
		return nil, &AmbiguousUserError{Value: query, Candidates: matches}
	}
}

func userMatches(user UserIdentity, query string, deployment Deployment) bool {
	values := []string{user.DisplayName, user.EmailAddress}
	if deployment == DeploymentCloud {
		// Cloud's query parameter searches display name and email. Stable
		// account IDs use ResolveAssignableAccountID instead.
	} else {
		// Data Center's username parameter is not a general display-name,
		// email, or immutable-key search.
		values = []string{user.Name}
	}
	for _, value := range values {
		if value != "" && strings.EqualFold(value, query) {
			return true
		}
	}
	return false
}

func userIdentityKey(user UserIdentity, deployment Deployment) string {
	if deployment == DeploymentCloud {
		return user.AccountID
	}
	return user.Name
}

func (c *Client) ResolveAssignableAccountID(ctx context.Context, issue, accountID string) (*UserIdentity, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, &ValidationError{Field: "account-id", Message: "must not be empty"}
	}
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	if deployment != DeploymentCloud {
		return nil, &ValidationError{Field: "account-id", Message: "is only valid on Jira Cloud"}
	}
	path, err := c.PlatformPath(ctx, 3, "/user/assignable/search")
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"issueKey":  {issue},
		"accountId": {accountID},
	}
	var users []UserIdentity
	if err := c.doRead(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &users); err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Active && users[i].AccountID == accountID {
			return &users[i], nil
		}
	}
	return nil, &ValidationError{Field: "account-id", Message: "no exact assignable user matched"}
}

func (c *Client) AssignIssue(ctx context.Context, issue, accountID, username string, unassign bool) (*AssignmentReceipt, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	path, err := c.PlatformPath(ctx, 3, "/issue/"+url.PathEscape(issue)+"/assignee")
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	receipt := &AssignmentReceipt{Issue: issue, Deployment: deployment, Unassigned: unassign, Assigned: !unassign}
	switch deployment {
	case DeploymentCloud:
		if username != "" {
			return nil, &ValidationError{Field: "user", Message: "Jira Cloud assignment requires an account ID"}
		}
		if !unassign && strings.TrimSpace(accountID) == "" {
			return nil, &ValidationError{Field: "account-id", Message: "must not be empty"}
		}
		if unassign {
			payload["accountId"] = nil
		} else {
			payload["accountId"] = accountID
			receipt.AccountID = accountID
		}
	case DeploymentDataCenter:
		if accountID != "" {
			return nil, &ValidationError{Field: "account-id", Message: "is only valid on Jira Cloud"}
		}
		if !unassign && strings.TrimSpace(username) == "" {
			return nil, &ValidationError{Field: "user", Message: "must not be empty"}
		}
		if unassign {
			payload["name"] = nil
		} else {
			payload["name"] = username
			receipt.User = username
		}
	default:
		return nil, &UnsupportedCapabilityError{Capability: "assignment", Deployment: deployment}
	}
	if err := c.do(ctx, http.MethodPut, path, payload, nil); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (c *Client) ValidateParentUpdate(ctx context.Context) error {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return err
	}
	if deployment != DeploymentCloud {
		return &UnsupportedCapabilityError{
			Capability: "high-level parent update",
			Deployment: deployment,
		}
	}
	return nil
}

func (c *Client) ResolveProjectIssueType(ctx context.Context, project, value string) (*IssueType, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	var issueTypes []IssueType
	if deployment == DeploymentCloud {
		issueTypes, err = c.allCloudCreateIssueTypes(ctx, project)
	} else {
		found, findErr := c.Project(ctx, project)
		err = findErr
		if found != nil {
			issueTypes = found.IssueTypes
		}
	}
	if err != nil {
		return nil, err
	}
	return resolveIssueType(issueTypes, value)
}
