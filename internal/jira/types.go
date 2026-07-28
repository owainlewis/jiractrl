package jira

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
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
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
	Description string       `json:"description"`
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
	Author  User   `json:"author"`
	Body    string `json:"body"`
	Created string `json:"created"`
	Updated string `json:"updated"`
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
