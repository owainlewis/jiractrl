package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Deployment string

const (
	DeploymentAuto       Deployment = "auto"
	DeploymentCloud      Deployment = "cloud"
	DeploymentDataCenter Deployment = "data_center"
)

type CapabilityStatus string

const (
	CapabilityAvailable CapabilityStatus = "available"
	CapabilityUnknown   CapabilityStatus = "unknown"
	CapabilityMissing   CapabilityStatus = "unavailable"
)

type Capabilities struct {
	Platform          CapabilityStatus `json:"platform"`
	Software          CapabilityStatus `json:"software"`
	ServiceManagement CapabilityStatus `json:"service_management"`
}

type SoftwareBoard struct {
	ID       int64          `json:"id"`
	Self     string         `json:"self,omitempty"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Location map[string]any `json:"location,omitempty"`
}

type SoftwareBoardPage struct {
	Boards []SoftwareBoard `json:"boards"`
	Page   DiscoveryPage   `json:"page"`
}

type SoftwareSprint struct {
	ID            int64  `json:"id"`
	Self          string `json:"self,omitempty"`
	State         string `json:"state"`
	Name          string `json:"name"`
	StartDate     string `json:"startDate,omitempty"`
	EndDate       string `json:"endDate,omitempty"`
	CompleteDate  string `json:"completeDate,omitempty"`
	OriginBoardID int64  `json:"originBoardId,omitempty"`
	Goal          string `json:"goal,omitempty"`
}

type SoftwareSprintPage struct {
	Sprints []SoftwareSprint `json:"sprints"`
	Page    DiscoveryPage    `json:"page"`
}

type SoftwareIssueOptions struct {
	MaxResults int
	Cursor     string
	JQL        string
	Fields     []string
}

type SoftwareIssuePage struct {
	Issues []RawIssue `json:"issues"`
	Page   SearchPage `json:"page"`
}

type SoftwareWriteReceipt struct {
	Operation string          `json:"operation"`
	Issues    []string        `json:"issues"`
	Accepted  bool            `json:"accepted"`
	Partial   bool            `json:"partial"`
	Details   json.RawMessage `json:"details,omitempty"`
}

type ServerInfo struct {
	BaseURL          string       `json:"baseUrl"`
	Version          string       `json:"version"`
	VersionNumbers   []int        `json:"versionNumbers,omitempty"`
	DeploymentType   string       `json:"deploymentType"`
	BuildNumber      int          `json:"buildNumber,omitempty"`
	ServerTitle      string       `json:"serverTitle,omitempty"`
	Deployment       Deployment   `json:"deployment"`
	DeploymentSource string       `json:"deploymentSource"`
	Capabilities     Capabilities `json:"capabilities"`
}

type User struct {
	AccountID    string `json:"accountId"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

type SearchResponse struct {
	Issues []Issue    `json:"issues"`
	Page   SearchPage `json:"page"`
}

type SearchPage struct {
	Returned int    `json:"returned"`
	Limit    int    `json:"limit"`
	Next     string `json:"next,omitempty"`
	HasMore  bool   `json:"hasMore"`
	Total    *int   `json:"total,omitempty"`
	Pages    int    `json:"pages"`
}

type SearchOptions struct {
	JQL               string
	Fields            []string
	MaxResults        int
	Cursor            string
	ReconcileIssueIDs []int64
}

type Issue struct {
	ID     string     `json:"id"`
	Key    string     `json:"key"`
	Fields IssueField `json:"fields"`
}

type RawIssue struct {
	ID     string         `json:"id"`
	Key    string         `json:"key"`
	Fields map[string]any `json:"fields"`
}

type IssueField struct {
	Summary     string       `json:"summary"`
	Description RichText     `json:"description"`
	Status      NamedValue   `json:"status"`
	Priority    NamedValue   `json:"priority"`
	IssueType   NamedValue   `json:"issuetype"`
	Assignee    User         `json:"assignee"`
	Labels      []string     `json:"labels"`
	Created     string       `json:"created"`
	Updated     string       `json:"updated"`
	Comment     CommentBlock `json:"comment"`
}

type NamedValue struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CommentBlock struct {
	Total    int       `json:"total"`
	Comments []Comment `json:"comments"`
}

type Comment struct {
	ID         string             `json:"id"`
	Author     User               `json:"author"`
	Body       RichText           `json:"body"`
	Visibility *CommentVisibility `json:"visibility,omitempty"`
	Created    string             `json:"created"`
	Updated    string             `json:"updated"`
}

type CommentVisibility struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	Identifier string `json:"identifier,omitempty"`
}

type CommentPage struct {
	Comments []Comment     `json:"comments"`
	Page     DiscoveryPage `json:"page"`
}

type RichText struct {
	value any
}

func (r *RichText) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		r.value = nil
		return nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	r.value = value
	return nil
}

func (r RichText) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.value)
}

func (r RichText) PlainText() string {
	return strings.TrimSpace(plainText(r.value))
}

func plainText(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []any:
		var text strings.Builder
		for _, item := range value {
			text.WriteString(plainText(item))
		}
		return text.String()
	case map[string]any:
		if value["type"] == "hardBreak" {
			return "\n"
		}
		if text, ok := value["text"].(string); ok {
			return text
		}
		text := plainText(value["content"])
		switch value["type"] {
		case "paragraph", "heading", "codeBlock", "listItem":
			return text + "\n"
		default:
			return text
		}
	default:
		return ""
	}
}

type CreatedIssue struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

type MutationRequest struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body"`
}

type TransitionResponse struct {
	Transitions []Transition `json:"transitions"`
}

type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   struct {
		Name string `json:"name"`
	} `json:"to"`
}

type Field struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Custom      bool     `json:"custom"`
	Order       int      `json:"orderable,omitempty"`
	ClauseNames []string `json:"clauseNames,omitempty"`
}

type DiscoveryPage struct {
	StartAt    int  `json:"startAt"`
	MaxResults int  `json:"maxResults"`
	Returned   int  `json:"returned"`
	Total      *int `json:"total,omitempty"`
	Next       int  `json:"next,omitempty"`
	HasMore    bool `json:"hasMore"`
}

type Project struct {
	ID         string      `json:"id"`
	Key        string      `json:"key"`
	Name       string      `json:"name"`
	IssueTypes []IssueType `json:"issueTypes,omitempty"`
}

type ProjectPage struct {
	Projects []Project     `json:"projects"`
	Page     DiscoveryPage `json:"page"`
}

type IssueType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Subtask     bool   `json:"subtask"`
}

type IssueTypePage struct {
	IssueTypes []IssueType   `json:"issueTypes"`
	Page       DiscoveryPage `json:"page"`
}

type FieldMetadata struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Required        bool   `json:"required"`
	Schema          any    `json:"schema,omitempty"`
	HasDefaultValue bool   `json:"hasDefaultValue"`
	DefaultValue    any    `json:"defaultValue"`
	AllowedValues   []any  `json:"allowedValues"`
}

type MetadataResponse struct {
	Project   *Project        `json:"project,omitempty"`
	IssueType *IssueType      `json:"issueType,omitempty"`
	Issue     string          `json:"issue,omitempty"`
	Fields    []FieldMetadata `json:"fields"`
	Page      DiscoveryPage   `json:"page"`
}

type UserIdentity struct {
	AccountID    string `json:"accountId,omitempty"`
	Key          string `json:"key,omitempty"`
	Name         string `json:"name,omitempty"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       bool   `json:"active"`
}

type UserPage struct {
	Users []UserIdentity `json:"users"`
	Page  DiscoveryPage  `json:"page"`
}

type AssignmentReceipt struct {
	Issue      string     `json:"issue"`
	Deployment Deployment `json:"deployment"`
	AccountID  string     `json:"accountId,omitempty"`
	User       string     `json:"user,omitempty"`
	Unassigned bool       `json:"unassigned"`
	Assigned   bool       `json:"assigned"`
}

type Worklog struct {
	ID               string             `json:"id"`
	Author           UserIdentity       `json:"author"`
	UpdateAuthor     UserIdentity       `json:"updateAuthor"`
	Comment          RichText           `json:"comment"`
	Started          string             `json:"started"`
	Created          string             `json:"created"`
	Updated          string             `json:"updated"`
	TimeSpent        string             `json:"timeSpent"`
	TimeSpentSeconds int64              `json:"timeSpentSeconds"`
	Visibility       *CommentVisibility `json:"visibility,omitempty"`
}

type WorklogPage struct {
	Worklogs []Worklog     `json:"worklogs"`
	Page     DiscoveryPage `json:"page"`
}

type Watchers struct {
	IsWatching bool           `json:"isWatching"`
	WatchCount int            `json:"watchCount"`
	Watchers   []UserIdentity `json:"watchers"`
}

type WatcherReceipt struct {
	Issue      string     `json:"issue"`
	Deployment Deployment `json:"deployment"`
	AccountID  string     `json:"accountId,omitempty"`
	User       string     `json:"user,omitempty"`
	Self       bool       `json:"self"`
	Watching   bool       `json:"watching"`
}

type AmbiguousUserError struct {
	Value      string
	Candidates []UserIdentity
}

func (e *AmbiguousUserError) Error() string {
	return fmt.Sprintf("user %q is ambiguous; use --account-id on Cloud or an exact username on Data Center", e.Value)
}

type IssueLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

type LinkedIssue struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

type IssueLink struct {
	ID           string        `json:"id"`
	Type         IssueLinkType `json:"type"`
	InwardIssue  *LinkedIssue  `json:"inwardIssue,omitempty"`
	OutwardIssue *LinkedIssue  `json:"outwardIssue,omitempty"`
}

type IssueLinkView struct {
	ID        string        `json:"id"`
	Type      IssueLinkType `json:"type"`
	Direction string        `json:"direction"`
	Relation  string        `json:"relation"`
	Issue     LinkedIssue   `json:"issue"`
}

type LinkReceipt struct {
	Accepted                   bool   `json:"accepted"`
	DuplicateRequestsSucceed   bool   `json:"duplicateRequestsSucceed"`
	ServerReturnsCreatedLinkID bool   `json:"serverReturnsCreatedLinkId"`
	OutwardIssue               string `json:"outwardIssue"`
	InwardIssue                string `json:"inwardIssue"`
	Type                       string `json:"type"`
}

type Attachment struct {
	ID       StringID     `json:"id"`
	Filename string       `json:"filename"`
	MimeType string       `json:"mimeType"`
	Size     int64        `json:"size"`
	Created  any          `json:"created,omitempty"`
	Author   UserIdentity `json:"author"`
	Content  string       `json:"content,omitempty"`
}

type StringID string

func (id *StringID) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*id = StringID(value)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("Jira ID must be a string or number: %w", err)
	}
	*id = StringID(number.String())
	return nil
}

type ChangeItem struct {
	Field     string `json:"field"`
	FieldID   string `json:"fieldId,omitempty"`
	FieldType string `json:"fieldtype,omitempty"`
	From      string `json:"from,omitempty"`
	FromValue string `json:"fromString,omitempty"`
	To        string `json:"to,omitempty"`
	ToValue   string `json:"toString,omitempty"`
}

type Changelog struct {
	ID      string       `json:"id"`
	Author  UserIdentity `json:"author"`
	Created string       `json:"created"`
	Items   []ChangeItem `json:"items"`
}

type ChangelogPage struct {
	Histories []Changelog   `json:"histories"`
	Page      DiscoveryPage `json:"page"`
	Scanned   int           `json:"scanned"`
	Fields    []string      `json:"fields,omitempty"`
}

type AmbiguousMatchError struct {
	Value      string
	Candidates []IssueType
}

func (e *AmbiguousMatchError) Error() string {
	return fmt.Sprintf("issue type %q is ambiguous; use an ID", e.Value)
}
