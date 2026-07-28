package jira

import (
	"bytes"
	"encoding/json"
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
	Author  User     `json:"author"`
	Body    RichText `json:"body"`
	Created string   `json:"created"`
	Updated string   `json:"updated"`
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
