package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestWorklogAndWatcherMutationsAreNeverRetried(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{name: "add worklog", call: func(c *Client) error {
			_, err := c.AddWorklog(context.Background(), "ENG-1", map[string]any{"timeSpent": "1h"}, url.Values{})
			return err
		}},
		{name: "update worklog", call: func(c *Client) error {
			_, err := c.UpdateWorklog(context.Background(), "ENG-1", "10", map[string]any{"timeSpent": "1h"}, url.Values{})
			return err
		}},
		{name: "add watcher", call: func(c *Client) error {
			_, err := c.AddWatcher(context.Background(), "ENG-1", "a1", false)
			return err
		}},
		{name: "remove watcher", call: func(c *Client) error {
			_, err := c.RemoveWatcher(context.Background(), "ENG-1", "a1", false)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
			client.SetRetryPolicy(RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})
			if err := test.call(client); err == nil {
				t.Fatal("expected error")
			}
			if requests != 1 {
				t.Fatalf("requests = %d", requests)
			}
		})
	}
}

func TestWorklogsPageMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startAt") != "2" || r.URL.Query().Get("maxResults") != "1" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"startAt":2,"maxResults":1,"total":4,"worklogs":[{"id":"3","timeSpent":"1h"}]}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	page, err := client.Worklogs(context.Background(), "ENG-1", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Page.HasMore || page.Page.Next != 3 || page.Worklogs[0].ID != "3" {
		t.Fatalf("page = %#v", page)
	}
}
