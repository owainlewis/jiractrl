package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type agentWorkflowEnvelope struct {
	OK   bool            `json:"ok"`
	Data json.RawMessage `json:"data"`
}

func TestAgentWorkflowDiscoversIdentifiersAndExhaustsPages(t *testing.T) {
	var mutations []string
	var createPayload map[string]any
	var transitionPayload map[string]any
	var assignmentPayload map[string]any
	var attachmentBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/serverInfo":
			_, _ = io.WriteString(w, `{
				"baseUrl":"https://jira.example.test",
				"version":"9.12.0",
				"deploymentType":"Data Center"
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/agile/1.0/board":
			_, _ = io.WriteString(w, `{"values":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/servicedeskapi/servicedesk":
			_, _ = io.WriteString(w, `{"values":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/myself":
			_, _ = io.WriteString(w, `{"name":"agent","displayName":"Jira Agent"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/project":
			_, _ = io.WriteString(w, `[
				{"id":"10000","key":"OPS","name":"Operations"},
				{"id":"10001","key":"ENG","name":"Engineering"}
			]`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/project/ENG":
			_, _ = io.WriteString(w, `{
				"id":"10001",
				"key":"ENG",
				"name":"Engineering",
				"issueTypes":[{"id":"10004","name":"Task","subtask":false}]
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/createmeta":
			_, _ = io.WriteString(w, `{
				"projects":[{
					"id":"10001",
					"key":"ENG",
					"name":"Engineering",
					"issuetypes":[{
						"id":"10004",
						"name":"Task",
						"subtask":false,
						"fields":{
							"summary":{"name":"Summary","required":true,"allowedValues":[]},
							"customfield_10042":{
								"name":"Impact",
								"required":true,
								"allowedValues":[{"id":"20001","value":"High"}]
							}
						}
					}]
				}]
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/user/assignable/search":
			if got := r.URL.Query().Get("username"); got != "alex" {
				t.Errorf("assignable user query = %q, want alex", got)
			}
			_, _ = io.WriteString(w, `[{
				"name":"alex",
				"key":"alex-key",
				"displayName":"Alex Example",
				"active":true
			}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue":
			if err := json.NewDecoder(r.Body).Decode(&createPayload); err != nil {
				t.Errorf("decode create payload: %v", err)
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			mutations = append(mutations, "create")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"10101","key":"ENG-1"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ENG-1/attachments":
			reader, err := r.MultipartReader()
			if err != nil {
				t.Errorf("read multipart request: %v", err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			part, err := nextFilePart(reader)
			if err != nil {
				t.Errorf("read attachment part: %v", err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(part)
			if err != nil {
				t.Errorf("read attachment: %v", err)
				http.Error(w, "bad attachment", http.StatusBadRequest)
				return
			}
			attachmentBody = string(body)
			mutations = append(mutations, "attachment")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `[{
				"id":"50001",
				"filename":"evidence.txt",
				"mimeType":"text/plain",
				"size":16
			}]`)
		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/2/issue/ENG-1" &&
			r.URL.Query().Get("fields") == "attachment":
			_, _ = io.WriteString(w, `{
				"fields":{"attachment":[{
					"id":"50001",
					"filename":"evidence.txt",
					"mimeType":"text/plain",
					"size":16
				}]}
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issueLinkType":
			_, _ = io.WriteString(w, `{
				"issueLinkTypes":[{
					"id":"10000",
					"name":"Blocks",
					"inward":"is blocked by",
					"outward":"blocks"
				}]
			}`)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issueLink":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode link payload: %v", err)
			}
			linkType := payload["type"].(map[string]any)["name"]
			if linkType != "Blocks" {
				t.Errorf("link type = %v, want discovered Blocks", linkType)
			}
			mutations = append(mutations, "link")
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/2/issue/ENG-1" &&
			r.URL.Query().Get("fields") == "issuelinks":
			_, _ = io.WriteString(w, `{
				"fields":{"issuelinks":[{
					"id":"60001",
					"type":{
						"id":"10000",
						"name":"Blocks",
						"inward":"is blocked by",
						"outward":"blocks"
					},
					"outwardIssue":{"id":"10102","key":"ENG-2"}
				}]}
			}`)
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/2/issue/ENG-1/assignee":
			if err := json.NewDecoder(r.Body).Decode(&assignmentPayload); err != nil {
				t.Errorf("decode assignment payload: %v", err)
			}
			mutations = append(mutations, "assign")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ENG-1/comment":
			mutations = append(mutations, "comment")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"70001","body":"Evidence attached."}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1/comment":
			_, _ = io.WriteString(w, `{
				"startAt":0,
				"maxResults":50,
				"total":1,
				"comments":[{
					"id":"70001",
					"author":{"name":"agent","displayName":"Jira Agent"},
					"body":"Evidence attached."
				}]
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1/transitions":
			_, _ = io.WriteString(w, `{
				"transitions":[{"id":"31","name":"In Progress","to":{"name":"In Progress"}}]
			}`)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ENG-1/transitions":
			if err := json.NewDecoder(r.Body).Decode(&transitionPayload); err != nil {
				t.Errorf("decode transition payload: %v", err)
			}
			mutations = append(mutations, "transition")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1/changelog":
			switch r.URL.Query().Get("startAt") {
			case "0":
				_, _ = io.WriteString(w, `{
					"startAt":0,
					"maxResults":1,
					"total":2,
					"histories":[{
						"id":"80001",
						"created":"2026-07-28T09:00:00.000+0000",
						"items":[{"field":"assignee","fromString":"","toString":"alex"}]
					}]
				}`)
			case "1":
				_, _ = io.WriteString(w, `{
					"startAt":1,
					"maxResults":1,
					"total":2,
					"histories":[{
						"id":"80002",
						"created":"2026-07-28T09:01:00.000+0000",
						"items":[{"field":"status","fromString":"Open","toString":"In Progress"}]
					}]
				}`)
			default:
				t.Errorf("unexpected changelog startAt %q", r.URL.Query().Get("startAt"))
				http.Error(w, "unexpected page", http.StatusBadRequest)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1":
			_, _ = io.WriteString(w, `{
				"id":"10101",
				"key":"ENG-1",
				"fields":{
					"summary":"Investigate queue latency",
					"status":{"id":"3","name":"In Progress"},
					"assignee":{"name":"alex","displayName":"Alex Example"},
					"labels":["agent-created"]
				}
			}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	config := fmt.Sprintf(`[jira]
base_url = %q
token = "fixture-token"
deployment = "data_center"
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	runAgentWorkflowJSON(t, configPath, "auth", "check", "--json")
	serverInfo := runAgentWorkflowJSON(t, configPath, "server-info", "--json")
	var info struct {
		Deployment   string `json:"deployment"`
		Capabilities struct {
			Software          string `json:"software"`
			ServiceManagement string `json:"service_management"`
		} `json:"capabilities"`
	}
	decodeWorkflowData(t, serverInfo, &info)
	if info.Deployment != "data_center" ||
		info.Capabilities.Software != "available" ||
		info.Capabilities.ServiceManagement != "available" {
		t.Fatalf("server info = %#v", info)
	}

	firstProjects := runAgentWorkflowJSON(
		t, configPath, "projects", "list", "--start", "0", "--max", "1", "--json",
	)
	var firstPage struct {
		Projects []struct {
			Key string `json:"key"`
		} `json:"projects"`
		Page struct {
			Next    int  `json:"next"`
			HasMore bool `json:"hasMore"`
		} `json:"page"`
	}
	decodeWorkflowData(t, firstProjects, &firstPage)
	if len(firstPage.Projects) != 1 || !firstPage.Page.HasMore {
		t.Fatalf("first project page = %#v", firstPage)
	}
	secondProjects := runAgentWorkflowJSON(
		t, configPath, "projects", "list",
		"--start", fmt.Sprint(firstPage.Page.Next), "--max", "1", "--json",
	)
	var secondPage struct {
		Projects []struct {
			Key string `json:"key"`
		} `json:"projects"`
		Page struct {
			HasMore bool `json:"hasMore"`
		} `json:"page"`
	}
	decodeWorkflowData(t, secondProjects, &secondPage)
	if len(secondPage.Projects) != 1 || secondPage.Projects[0].Key != "ENG" || secondPage.Page.HasMore {
		t.Fatalf("second project page = %#v", secondPage)
	}
	projectKey := secondPage.Projects[0].Key

	runAgentWorkflowJSON(t, configPath, "projects", "get", projectKey, "--json")
	issueTypes := runAgentWorkflowJSON(t, configPath, "projects", "issue-types", projectKey, "--json")
	var discoveredTypes struct {
		IssueTypes []struct {
			ID string `json:"id"`
		} `json:"issueTypes"`
	}
	decodeWorkflowData(t, issueTypes, &discoveredTypes)
	issueTypeID := discoveredTypes.IssueTypes[0].ID

	metadata := runAgentWorkflowJSON(
		t, configPath, "meta", "create", "--project", projectKey, "--type", issueTypeID, "--json",
	)
	var discoveredMetadata struct {
		Fields []struct {
			ID            string `json:"id"`
			AllowedValues []struct {
				ID string `json:"id"`
			} `json:"allowedValues"`
		} `json:"fields"`
	}
	decodeWorkflowData(t, metadata, &discoveredMetadata)
	customFieldID, allowedValueID := "", ""
	for _, field := range discoveredMetadata.Fields {
		if strings.HasPrefix(field.ID, "customfield_") && len(field.AllowedValues) > 0 {
			customFieldID = field.ID
			allowedValueID = field.AllowedValues[0].ID
		}
	}
	if customFieldID == "" || allowedValueID == "" {
		t.Fatalf("metadata did not expose required identifiers: %#v", discoveredMetadata)
	}

	users := runAgentWorkflowJSON(
		t, configPath, "users", "assignable", "--project", projectKey, "--query", "alex", "--json",
	)
	var discoveredUsers struct {
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	decodeWorkflowData(t, users, &discoveredUsers)
	username := discoveredUsers.Users[0].Name

	createInput := filepath.Join(dir, "create.json")
	createBody := map[string]any{"fields": map[string]any{
		"project":     map[string]string{"key": projectKey},
		"issuetype":   map[string]string{"id": issueTypeID},
		"summary":     "Investigate queue latency",
		customFieldID: map[string]string{"id": allowedValueID},
		"labels":      []string{"agent-created"},
	}}
	encodedCreate, err := json.Marshal(createBody)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(createInput, encodedCreate, 0o600); err != nil {
		t.Fatal(err)
	}
	created := runAgentWorkflowJSON(t, configPath, "create", "--input", createInput, "--json")
	var createdIssue struct {
		Key string `json:"key"`
	}
	decodeWorkflowData(t, created, &createdIssue)
	if createdIssue.Key != "ENG-1" {
		t.Fatalf("created issue = %#v", createdIssue)
	}

	attachmentPath := filepath.Join(dir, "evidence.txt")
	if err := os.WriteFile(attachmentPath, []byte("workflow evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	runAgentWorkflowJSON(
		t, configPath, "attachments", "upload", createdIssue.Key,
		"--file", attachmentPath, "--json",
	)
	attachments := runAgentWorkflowJSON(
		t, configPath, "attachments", "list", createdIssue.Key, "--json",
	)
	var verifiedAttachments struct {
		Attachments []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		} `json:"attachments"`
	}
	decodeWorkflowData(t, attachments, &verifiedAttachments)
	if len(verifiedAttachments.Attachments) != 1 ||
		verifiedAttachments.Attachments[0].ID != "50001" ||
		verifiedAttachments.Attachments[0].Filename != "evidence.txt" {
		t.Fatalf("listed attachments = %#v", verifiedAttachments)
	}

	linkTypes := runAgentWorkflowJSON(t, configPath, "links", "types", "--json")
	var discoveredLinks struct {
		Types []struct {
			Name string `json:"name"`
		} `json:"types"`
	}
	decodeWorkflowData(t, linkTypes, &discoveredLinks)
	linkTypeName := discoveredLinks.Types[0].Name
	runAgentWorkflowJSON(
		t, configPath, "links", "add",
		"--type", linkTypeName, "--outward", createdIssue.Key, "--inward", "ENG-2", "--json",
	)
	links := runAgentWorkflowJSON(t, configPath, "links", "list", createdIssue.Key, "--json")
	var verifiedLinks struct {
		Links []struct {
			ID        string `json:"id"`
			Direction string `json:"direction"`
			Issue     struct {
				Key string `json:"key"`
			} `json:"issue"`
		} `json:"links"`
	}
	decodeWorkflowData(t, links, &verifiedLinks)
	if len(verifiedLinks.Links) != 1 ||
		verifiedLinks.Links[0].ID != "60001" ||
		verifiedLinks.Links[0].Direction != "outward" ||
		verifiedLinks.Links[0].Issue.Key != "ENG-2" {
		t.Fatalf("listed links = %#v", verifiedLinks)
	}

	runAgentWorkflowJSON(
		t, configPath, "assign", createdIssue.Key, "--user", username, "--json",
	)
	runAgentWorkflowJSON(
		t, configPath, "comments", "add", createdIssue.Key,
		"--body", "Evidence attached.", "--json",
	)
	comments := runAgentWorkflowJSON(
		t, configPath, "comments", "list", createdIssue.Key,
		"--all", "--limit", "10", "--json",
	)
	var verifiedComments struct {
		Comments []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"comments"`
		Page struct {
			HasMore bool `json:"hasMore"`
		} `json:"page"`
	}
	decodeWorkflowData(t, comments, &verifiedComments)
	if len(verifiedComments.Comments) != 1 ||
		verifiedComments.Comments[0].ID != "70001" ||
		verifiedComments.Comments[0].Body != "Evidence attached." ||
		verifiedComments.Page.HasMore {
		t.Fatalf("listed comments = %#v", verifiedComments)
	}

	transitions := runAgentWorkflowJSON(t, configPath, "transitions", createdIssue.Key, "--json")
	var discoveredTransitions struct {
		Transitions []struct {
			ID string `json:"id"`
		} `json:"transitions"`
	}
	decodeWorkflowData(t, transitions, &discoveredTransitions)
	transitionID := discoveredTransitions.Transitions[0].ID
	runAgentWorkflowJSON(
		t, configPath, "transition", createdIssue.Key, "--to", transitionID, "--json",
	)

	changelog := runAgentWorkflowJSON(
		t, configPath, "changelog", createdIssue.Key,
		"--max", "1", "--all", "--limit", "2", "--json",
	)
	var completeHistory struct {
		Histories []struct {
			ID string `json:"id"`
		} `json:"histories"`
		Page struct {
			Returned int  `json:"returned"`
			HasMore  bool `json:"hasMore"`
		} `json:"page"`
		Scanned int `json:"scanned"`
	}
	decodeWorkflowData(t, changelog, &completeHistory)
	if len(completeHistory.Histories) != 2 ||
		completeHistory.Page.Returned != 2 ||
		completeHistory.Page.HasMore ||
		completeHistory.Scanned != 2 {
		t.Fatalf("changelog did not exhaust pages: %#v", completeHistory)
	}

	finalIssue := runAgentWorkflowJSON(t, configPath, "get", createdIssue.Key, "--json")
	var verifiedIssue struct {
		Key    string `json:"key"`
		Fields struct {
			Status struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"fields"`
	}
	decodeWorkflowData(t, finalIssue, &verifiedIssue)
	if verifiedIssue.Key != "ENG-1" || verifiedIssue.Fields.Status.Name != "In Progress" {
		t.Fatalf("final issue = %#v", verifiedIssue)
	}

	if got := createPayload["fields"].(map[string]any)["issuetype"].(map[string]any)["id"]; got != issueTypeID {
		t.Fatalf("created issue type = %v, want discovered %s", got, issueTypeID)
	}
	if got := createPayload["fields"].(map[string]any)[customFieldID].(map[string]any)["id"]; got != allowedValueID {
		t.Fatalf("custom field value = %v, want discovered %s", got, allowedValueID)
	}
	if assignmentPayload["name"] != username {
		t.Fatalf("assignment = %#v, want discovered user %q", assignmentPayload, username)
	}
	if got := transitionPayload["transition"].(map[string]any)["id"]; got != transitionID {
		t.Fatalf("transition = %v, want discovered %s", got, transitionID)
	}
	if attachmentBody != "workflow evidence" {
		t.Fatalf("attachment body = %q", attachmentBody)
	}
	wantMutations := []string{"create", "attachment", "link", "assign", "comment", "transition"}
	if !reflect.DeepEqual(mutations, wantMutations) {
		t.Fatalf("mutations = %#v, want %#v", mutations, wantMutations)
	}
}

func runAgentWorkflowJSON(t *testing.T, configPath string, args ...string) agentWorkflowEnvelope {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := append([]string{"--config", configPath}, args...)
	if err := Run(command, &stdout, &stderr); err != nil {
		t.Fatalf("jiractrl %s: %v\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	var envelope agentWorkflowEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("jiractrl %s returned invalid JSON: %v\nstdout: %s",
			strings.Join(args, " "), err, stdout.String())
	}
	if !envelope.OK || len(envelope.Data) == 0 {
		t.Fatalf("jiractrl %s returned unsuccessful envelope: %s", strings.Join(args, " "), stdout.String())
	}
	return envelope
}

func decodeWorkflowData(t *testing.T, envelope agentWorkflowEnvelope, target any) {
	t.Helper()
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("decode workflow data: %v\ndata: %s", err, string(envelope.Data))
	}
}

func nextFilePart(reader *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil, err
		}
		if part.FileName() != "" {
			return part, nil
		}
	}
}
