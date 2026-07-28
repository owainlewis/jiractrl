package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChangelogPaginationAndFieldFiltering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/ENG-1/changelog" ||
			r.URL.Query().Get("startAt") != "2" ||
			r.URL.Query().Get("maxResults") != "2" {
			t.Fatalf("request = %s", r.URL.RequestURI())
		}
		_, _ = w.Write([]byte(`{
			"startAt":2,
			"maxResults":2,
			"total":5,
			"isLast":false,
			"values":[
				{"id":"3","items":[
					{"field":"status","fieldId":"status","fromString":"Open","toString":"Done"},
					{"field":"Priority","fieldId":"priority","fromString":"Low","toString":"High"}
				]},
				{"id":"4","items":[{"field":"assignee","fieldId":"assignee","toString":"Ada"}]}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	page, err := client.Changelog(context.Background(), "ENG-1", 2, 2, []string{"PRIORITY"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Scanned != 2 || len(page.Histories) != 1 || len(page.Histories[0].Items) != 1 ||
		page.Histories[0].Items[0].FieldID != "priority" {
		t.Fatalf("page = %#v", page)
	}
	if !page.Page.HasMore || page.Page.Next != 4 || page.Page.Returned != 1 {
		t.Fatalf("metadata = %#v", page.Page)
	}
}

func TestDataCenterChangelogAcceptsHistoriesShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue/ENG-1/changelog" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":50,"total":1,"histories":[{"id":"1","items":[{"field":"status"}]}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	page, err := client.Changelog(context.Background(), "ENG-1", 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Histories) != 1 || page.Page.HasMore {
		t.Fatalf("page = %#v", page)
	}
}
