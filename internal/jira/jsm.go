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

const JSMReadPageLimit = 100

const defaultJSMRequestExpand = "participant,status,sla,requestType,serviceDesk"

type PermissionError struct {
	Operation string
	Err       error
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("permission denied for %s: %v", e.Operation, e.Err)
}

func (e *PermissionError) Unwrap() error { return e.Err }

type JSMRequestOptions struct {
	Start            int
	Limit            int
	ServiceDeskID    string
	RequestTypeID    string
	RequestStatus    string
	RequestOwnership string
	SearchTerm       string
	Expand           string
}

type JSMRequestTypeField struct {
	FieldID     string            `json:"fieldId"`
	Name        string            `json:"name"`
	Required    bool              `json:"required"`
	ValidValues []json.RawMessage `json:"validValues"`
	JiraSchema  json.RawMessage   `json:"jiraSchema"`
}

type JSMRequestTypeFields struct {
	RequestTypeFields         []JSMRequestTypeField `json:"requestTypeFields"`
	CanRaiseOnBehalfOf        bool                  `json:"canRaiseOnBehalfOf"`
	CanAddRequestParticipants bool                  `json:"canAddRequestParticipants"`
}

type JSMParticipantInput struct {
	AccountIDs []string
	Usernames  []string
}

func (c *Client) ServiceManagementCapability(ctx context.Context) (CapabilityStatus, error) {
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return CapabilityUnknown, err
	}
	return c.serviceManagementCapability(ctx, deployment)
}

func (c *Client) serviceManagementCapability(ctx context.Context, deployment Deployment) (CapabilityStatus, error) {
	c.jsmOnce.Do(func() {
		var result json.RawMessage
		mode := authBearer
		if deployment == DeploymentCloud && c.email != "" {
			mode = authBasic
		}
		err := c.doReadWithAuth(ctx, http.MethodGet, "/rest/servicedeskapi/servicedesk?limit=1", nil, &result, mode)
		if err == nil {
			c.jsmStatus = CapabilityAvailable
			return
		}
		var jiraErr *Error
		if errors.As(err, &jiraErr) && jiraErr.StatusCode == http.StatusNotFound {
			c.jsmStatus = CapabilityMissing
			return
		}
		c.jsmStatus = CapabilityUnknown
		c.jsmErr = wrapJSMPermission("detect Jira Service Management", err)
	})
	return c.jsmStatus, c.jsmErr
}

func (c *Client) RequireServiceManagement(ctx context.Context) error {
	status, err := c.ServiceManagementCapability(ctx)
	if err != nil {
		return err
	}
	return c.RequireCapability(ctx, "jira_service_management", status)
}

func (c *Client) JSMServiceDesks(ctx context.Context, start, limit int) (json.RawMessage, error) {
	if err := validateJSMPage(start, limit); err != nil {
		return nil, err
	}
	query := url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
	return c.jsmRead(ctx, "list service desks", "/rest/servicedeskapi/servicedesk?"+query.Encode())
}

func (c *Client) JSMServiceDesk(ctx context.Context, serviceDeskID string) (json.RawMessage, error) {
	id, err := validateJSMID("service desk", serviceDeskID)
	if err != nil {
		return nil, err
	}
	return c.jsmRead(ctx, "get service desk", "/rest/servicedeskapi/servicedesk/"+url.PathEscape(id))
}

func (c *Client) JSMQueues(ctx context.Context, serviceDeskID string, start, limit int, includeCount bool) (json.RawMessage, error) {
	id, err := validateJSMID("service desk", serviceDeskID)
	if err != nil {
		return nil, err
	}
	if err := validateJSMPage(start, limit); err != nil {
		return nil, err
	}
	query := url.Values{
		"start":        {strconv.Itoa(start)},
		"limit":        {strconv.Itoa(limit)},
		"includeCount": {strconv.FormatBool(includeCount)},
	}
	path := fmt.Sprintf("/rest/servicedeskapi/servicedesk/%s/queue?%s", url.PathEscape(id), query.Encode())
	return c.jsmRead(ctx, "list queues", path)
}

func (c *Client) JSMRequestTypes(ctx context.Context, serviceDeskID string, start, limit int) (json.RawMessage, error) {
	id, err := validateJSMID("service desk", serviceDeskID)
	if err != nil {
		return nil, err
	}
	if err := validateJSMPage(start, limit); err != nil {
		return nil, err
	}
	query := url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
	path := fmt.Sprintf("/rest/servicedeskapi/servicedesk/%s/requesttype?%s", url.PathEscape(id), query.Encode())
	return c.jsmRead(ctx, "list request types", path)
}

func (c *Client) JSMRequestTypeFields(ctx context.Context, serviceDeskID, requestTypeID string) (json.RawMessage, error) {
	serviceDesk, requestType, err := validateJSMRequestTypeIDs(serviceDeskID, requestTypeID)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf(
		"/rest/servicedeskapi/servicedesk/%s/requesttype/%s/field",
		url.PathEscape(serviceDesk), url.PathEscape(requestType),
	)
	return c.jsmRead(ctx, "get request type fields", path)
}

func (c *Client) JSMRequests(ctx context.Context, options JSMRequestOptions) (json.RawMessage, error) {
	if err := validateJSMPage(options.Start, options.Limit); err != nil {
		return nil, err
	}
	query := url.Values{
		"start":  {strconv.Itoa(options.Start)},
		"limit":  {strconv.Itoa(options.Limit)},
		"expand": {normalizeJSMExpand(options.Expand)},
	}
	if options.ServiceDeskID != "" {
		id, err := validateJSMID("service desk", options.ServiceDeskID)
		if err != nil {
			return nil, err
		}
		query.Set("serviceDeskId", id)
	}
	if options.RequestTypeID != "" {
		id, err := validateJSMID("request type", options.RequestTypeID)
		if err != nil {
			return nil, err
		}
		query.Set("requestTypeId", id)
	}
	if value := strings.TrimSpace(options.RequestStatus); value != "" {
		query.Set("requestStatus", value)
	}
	if value := strings.TrimSpace(options.RequestOwnership); value != "" {
		query.Set("requestOwnership", value)
	}
	if value := strings.TrimSpace(options.SearchTerm); value != "" {
		query.Set("searchTerm", value)
	}
	return c.jsmRead(ctx, "list requests", "/rest/servicedeskapi/request?"+query.Encode())
}

func (c *Client) JSMRequest(ctx context.Context, issueIDOrKey, expand string) (json.RawMessage, error) {
	issue, err := validateJSMIssue(issueIDOrKey)
	if err != nil {
		return nil, err
	}
	query := url.Values{"expand": {normalizeJSMExpand(expand)}}
	return c.jsmRead(ctx, "get request", "/rest/servicedeskapi/request/"+url.PathEscape(issue)+"?"+query.Encode())
}

func (c *Client) JSMRequestSLAs(ctx context.Context, issueIDOrKey string, start, limit int) (json.RawMessage, error) {
	issue, err := validateJSMIssue(issueIDOrKey)
	if err != nil {
		return nil, err
	}
	if err := validateJSMPage(start, limit); err != nil {
		return nil, err
	}
	query := url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
	return c.jsmRead(ctx, "list request SLAs", "/rest/servicedeskapi/request/"+url.PathEscape(issue)+"/sla?"+query.Encode())
}

func (c *Client) CreateJSMRequest(
	ctx context.Context,
	serviceDeskID, requestTypeID string,
	fieldValues map[string]any,
) (json.RawMessage, error) {
	serviceDesk, requestType, err := validateJSMRequestTypeIDs(serviceDeskID, requestTypeID)
	if err != nil {
		return nil, err
	}
	if fieldValues == nil {
		return nil, &ValidationError{Field: "input", Message: "must contain a JSON object of request field values"}
	}
	rawFields, err := c.JSMRequestTypeFields(ctx, serviceDesk, requestType)
	if err != nil {
		return nil, err
	}
	var metadata JSMRequestTypeFields
	if err := json.Unmarshal(rawFields, &metadata); err != nil {
		return nil, fmt.Errorf("decode Jira Service Management request field metadata: %w", err)
	}
	if err := validateJSMRequestFieldValues(metadata.RequestTypeFields, fieldValues); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"serviceDeskId":      serviceDesk,
		"requestTypeId":      requestType,
		"requestFieldValues": fieldValues,
	}
	return c.jsmWrite(ctx, "create request", http.MethodPost, "/rest/servicedeskapi/request", payload)
}

func (c *Client) JSMRequestComments(ctx context.Context, issueIDOrKey string, start, limit int) (json.RawMessage, error) {
	issue, err := validateJSMIssue(issueIDOrKey)
	if err != nil {
		return nil, err
	}
	if err := validateJSMPage(start, limit); err != nil {
		return nil, err
	}
	query := url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
	path := "/rest/servicedeskapi/request/" + url.PathEscape(issue) + "/comment?" + query.Encode()
	return c.jsmRead(ctx, "list request comments", path)
}

func (c *Client) AddJSMRequestComment(ctx context.Context, issueIDOrKey, body string, public *bool) (json.RawMessage, error) {
	issue, err := validateJSMIssue(issueIDOrKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, &ValidationError{Field: "body", Message: "must not be empty"}
	}
	if public == nil {
		return nil, &ValidationError{Field: "visibility", Message: "must explicitly be public or internal"}
	}
	payload := map[string]any{"body": body, "public": *public}
	path := "/rest/servicedeskapi/request/" + url.PathEscape(issue) + "/comment"
	result, err := c.jsmWrite(ctx, "add request comment", http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}
	var confirmation struct {
		Public *bool `json:"public"`
	}
	if err := json.Unmarshal(result, &confirmation); err != nil || confirmation.Public == nil {
		return nil, errors.New("Jira Service Management comment response did not confirm visibility")
	}
	if *confirmation.Public != *public {
		return nil, errors.New("Jira Service Management comment response visibility did not match the request")
	}
	return result, nil
}

func (c *Client) JSMRequestParticipants(ctx context.Context, issueIDOrKey string, start, limit int) (json.RawMessage, error) {
	issue, err := validateJSMIssue(issueIDOrKey)
	if err != nil {
		return nil, err
	}
	if err := validateJSMPage(start, limit); err != nil {
		return nil, err
	}
	query := url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
	path := "/rest/servicedeskapi/request/" + url.PathEscape(issue) + "/participant?" + query.Encode()
	return c.jsmRead(ctx, "list request participants", path)
}

func (c *Client) ChangeJSMRequestParticipants(
	ctx context.Context,
	issueIDOrKey string,
	input JSMParticipantInput,
	remove bool,
) (json.RawMessage, error) {
	issue, err := validateJSMIssue(issueIDOrKey)
	if err != nil {
		return nil, err
	}
	if err := c.RequireServiceManagement(ctx); err != nil {
		return nil, err
	}
	deployment, err := c.Deployment(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := jsmParticipantPayload(deployment, input)
	if err != nil {
		return nil, err
	}
	method := http.MethodPost
	operation := "add request participants"
	if remove {
		method = http.MethodDelete
		operation = "remove request participants"
	}
	path := "/rest/servicedeskapi/request/" + url.PathEscape(issue) + "/participant"
	return c.jsmWriteAfterCapability(ctx, operation, method, path, payload)
}

func (c *Client) jsmRead(ctx context.Context, operation, path string) (json.RawMessage, error) {
	if err := c.RequireServiceManagement(ctx); err != nil {
		return nil, err
	}
	var result json.RawMessage
	if err := c.doRead(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, wrapJSMPermission(operation, err)
	}
	return result, nil
}

func (c *Client) jsmWrite(ctx context.Context, operation, method, path string, payload map[string]any) (json.RawMessage, error) {
	if err := c.RequireServiceManagement(ctx); err != nil {
		return nil, err
	}
	return c.jsmWriteAfterCapability(ctx, operation, method, path, payload)
}

func (c *Client) jsmWriteAfterCapability(
	ctx context.Context,
	operation, method, path string,
	payload map[string]any,
) (json.RawMessage, error) {
	_, result, err := c.doRaw(ctx, method, path, payload)
	if err != nil {
		return nil, wrapJSMPermission(operation, err)
	}
	return result, nil
}

func wrapJSMPermission(operation string, err error) error {
	var jiraErr *Error
	if errors.As(err, &jiraErr) && jiraErr.StatusCode == http.StatusForbidden {
		return &PermissionError{Operation: operation, Err: err}
	}
	return err
}

func validateJSMPage(start, limit int) error {
	if start < 0 {
		return &ValidationError{Field: "start", Message: "must be non-negative"}
	}
	if limit < 1 || limit > JSMReadPageLimit {
		return &ValidationError{Field: "max", Message: fmt.Sprintf("must be between 1 and %d", JSMReadPageLimit)}
	}
	return nil
}

func validateJSMID(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return "", &ValidationError{Field: field, Message: "must be a positive integer"}
	}
	return value, nil
}

func validateJSMRequestTypeIDs(serviceDeskID, requestTypeID string) (string, string, error) {
	serviceDesk, err := validateJSMID("service desk", serviceDeskID)
	if err != nil {
		return "", "", err
	}
	requestType, err := validateJSMID("request type", requestTypeID)
	if err != nil {
		return "", "", err
	}
	return serviceDesk, requestType, nil
}

func validateJSMIssue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &ValidationError{Field: "request", Message: "must not be empty"}
	}
	return value, nil
}

func normalizeJSMExpand(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultJSMRequestExpand
	}
	return value
}

func validateJSMRequestFieldValues(fields []JSMRequestTypeField, values map[string]any) error {
	known := make(map[string]JSMRequestTypeField, len(fields))
	for _, field := range fields {
		known[field.FieldID] = field
		if field.Required {
			value, ok := values[field.FieldID]
			if !ok || emptyJSMFieldValue(value) {
				return &ValidationError{
					Field:   "requestFieldValues." + field.FieldID,
					Message: fmt.Sprintf("is required by request type (%s)", field.Name),
				}
			}
		}
	}
	for fieldID := range values {
		if _, ok := known[fieldID]; !ok {
			return &ValidationError{
				Field:   "requestFieldValues." + fieldID,
				Message: "is not available for this request type",
			}
		}
	}
	return nil
}

func emptyJSMFieldValue(value any) bool {
	if value == nil {
		return true
	}
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value) == ""
	case []any:
		return len(value) == 0
	case map[string]any:
		return len(value) == 0
	default:
		return false
	}
}

func jsmParticipantPayload(deployment Deployment, input JSMParticipantInput) (map[string]any, error) {
	accountIDs, err := cleanJSMIdentities("account-id", input.AccountIDs)
	if err != nil {
		return nil, err
	}
	usernames, err := cleanJSMIdentities("username", input.Usernames)
	if err != nil {
		return nil, err
	}
	switch deployment {
	case DeploymentCloud:
		if len(accountIDs) == 0 || len(usernames) > 0 {
			return nil, &ValidationError{
				Field:   "participant",
				Message: "Cloud requires one or more --account-id values and does not accept --username",
			}
		}
		return map[string]any{"accountIds": accountIDs}, nil
	case DeploymentDataCenter:
		if len(usernames) == 0 || len(accountIDs) > 0 {
			return nil, &ValidationError{
				Field:   "participant",
				Message: "Data Center requires one or more --username values and does not accept --account-id",
			}
		}
		return map[string]any{"usernames": usernames}, nil
	default:
		return nil, &UnsupportedCapabilityError{Capability: "JSM request participant identity", Deployment: deployment}
	}
}

func cleanJSMIdentities(field string, values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				return nil, &ValidationError{Field: field, Message: "values must not be empty"}
			}
			result = append(result, item)
		}
	}
	return result, nil
}
