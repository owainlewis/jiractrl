package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudDiscoveryUsesCurrentMetadataEndpoints(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/rest/api/3/project/search":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":1,"total":2,"isLast":false,"values":[{"id":"10000","key":"ENG","name":"Engineering"}]}`))
		case "/rest/api/3/project/ENG":
			_, _ = w.Write([]byte(`{"id":"10000","key":"ENG","name":"Engineering"}`))
		case "/rest/api/3/issue/createmeta/ENG/issuetypes":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":3,"isLast":true,"issueTypes":[
				{"id":"10001","name":"Task","subtask":false},
				{"id":"10002","name":"Task","subtask":true},
				{"id":"10003","name":"Bug","subtask":false}
			]}`))
		case "/rest/api/3/issue/createmeta/ENG/issuetypes/10003":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":3,"isLast":true,"fields":[
				{"fieldId":"customfield_10010","key":"customfield_10010","name":"Required select","required":true,"schema":{"type":"option","custom":"select"},"hasDefaultValue":true,"defaultValue":{"id":"1"},"allowedValues":[{"id":"1","value":"One"}]},
				{"fieldId":"customfield_10011","name":"User","required":false,"schema":{"type":"user"},"hasDefaultValue":false,"allowedValues":[]},
				{"fieldId":"summary","name":"Summary","required":true,"schema":{"type":"string"},"hasDefaultValue":false}
			]}`))
		case "/rest/api/3/issue/ENG-1/editmeta":
			_, _ = w.Write([]byte(`{"fields":{"summary":{"name":"Summary","required":true,"schema":{"type":"string"},"hasDefaultValue":false,"allowedValues":[]}}}`))
		case "/rest/api/3/user/assignable/search":
			_, _ = w.Write([]byte(`[{"accountId":"abc123","displayName":"Cloud User","active":true}]`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	ctx := context.Background()

	projects, err := client.Projects(ctx, "eng", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !projects.Page.HasMore || projects.Page.Next != 1 || projects.Projects[0].Key != "ENG" {
		t.Fatalf("projects = %#v", projects)
	}
	project, err := client.Project(ctx, "ENG")
	if err != nil || project.ID != "10000" {
		t.Fatalf("project = %#v, err = %v", project, err)
	}
	types, err := client.ProjectIssueTypes(ctx, "ENG", 0, 50)
	if err != nil || len(types.IssueTypes) != 3 {
		t.Fatalf("types = %#v, err = %v", types, err)
	}

	_, err = client.CreateMetadata(ctx, "ENG", "Task", 0, 50)
	var ambiguous *AmbiguousMatchError
	if !errors.As(err, &ambiguous) || len(ambiguous.Candidates) != 2 {
		t.Fatalf("ambiguous error = %#v", err)
	}
	meta, err := client.CreateMetadata(ctx, "ENG", "10003", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Fields) != 3 || meta.Fields[0].ID != "customfield_10010" {
		t.Fatalf("meta = %#v", meta)
	}
	if !meta.Fields[0].Required || !meta.Fields[0].HasDefaultValue ||
		len(meta.Fields[0].AllowedValues) != 1 {
		t.Fatalf("required select = %#v", meta.Fields[0])
	}
	if meta.Fields[1].AllowedValues == nil || len(meta.Fields[1].AllowedValues) != 0 {
		t.Fatalf("empty allowed values = %#v", meta.Fields[1].AllowedValues)
	}
	edit, err := client.EditMetadata(ctx, "ENG-1")
	if err != nil || edit.Fields[0].ID != "summary" {
		t.Fatalf("edit = %#v, err = %v", edit, err)
	}
	users, err := client.AssignableUsers(ctx, "ENG", "", "cloud", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if users.Users[0].AccountID != "abc123" || users.Users[0].Name != "" || users.Users[0].Key != "" {
		t.Fatalf("Cloud identity = %#v", users.Users[0])
	}
	for _, path := range paths {
		if path == "/rest/api/3/issue/createmeta" {
			t.Fatal("used deprecated broad Cloud create metadata endpoint")
		}
	}
}

func TestDataCenterDiscoveryPreservesServerUserIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/project":
			_, _ = w.Write([]byte(`[
				{"id":"1","key":"ENG","name":"Engineering"},
				{"id":"2","key":"OPS","name":"Operations"}
			]`))
		case "/rest/api/2/project/ENG":
			_, _ = w.Write([]byte(`{"id":"1","key":"ENG","name":"Engineering","issueTypes":[{"id":"10","name":"Task","subtask":false}]}`))
		case "/rest/api/2/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"1","key":"ENG","name":"Engineering","issuetypes":[
				{"id":"10","name":"Task","subtask":false,"fields":{
					"customfield_10010":{"name":"Required select","required":true,"schema":{"type":"option"},"hasDefaultValue":false,"allowedValues":[{"id":"1"}]},
					"customfield_10011":{"name":"User","required":false,"schema":{"type":"user"},"hasDefaultValue":false}
				}}
			]}]}`))
		case "/rest/api/2/user/assignable/search":
			_, _ = w.Write([]byte(`[{"key":"jdoe","name":"john","displayName":"John Doe","active":true}]`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	ctx := context.Background()
	projects, err := client.Projects(ctx, "e", 0, 1)
	if err != nil || len(projects.Projects) != 1 || !projects.Page.HasMore {
		t.Fatalf("projects = %#v, err = %v", projects, err)
	}
	types, err := client.ProjectIssueTypes(ctx, "ENG", 0, 50)
	if err != nil || types.IssueTypes[0].ID != "10" {
		t.Fatalf("types = %#v, err = %v", types, err)
	}
	meta, err := client.CreateMetadata(ctx, "ENG", "Task", 0, 50)
	if err != nil || len(meta.Fields) != 2 || meta.Fields[1].AllowedValues == nil {
		t.Fatalf("meta = %#v, err = %v", meta, err)
	}
	users, err := client.AssignableUsers(ctx, "", "ENG-1", "john", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	user := users.Users[0]
	if user.AccountID != "" || user.Name != "john" || user.Key != "jdoe" {
		t.Fatalf("Data Center identity = %#v", user)
	}
	if got := strings.TrimSpace(user.DisplayName); got != "John Doe" {
		t.Fatalf("display name = %q", got)
	}
}
