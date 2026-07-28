package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSafeReadRetriesRateLimitAndCapturesMetadata(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests < 3 {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("RateLimit-Reason", "jira-burst-based")
			w.Header().Set("X-RateLimit-Limit", "100")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "2026-07-28T12:00Z")
			w.Header().Set("X-RateLimit-NearLimit", "true")
			http.Error(w, `{"errorMessages":["slow down"]}`, http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"displayName":"Agent"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Second})
	var delays []time.Duration
	client.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	user, err := client.Myself(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Agent" {
		t.Fatalf("user = %#v", user)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if !reflect.DeepEqual(delays, []time.Duration{time.Second, time.Second}) {
		t.Fatalf("delays = %#v", delays)
	}
}

func TestSafeReadUsesBoundedExponentialBackoff(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    150 * time.Millisecond,
	})
	client.jitter = func(delay time.Duration) time.Duration { return delay }
	var delays []time.Duration
	client.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	_, err := client.Myself(context.Background())
	var jiraErr *Error
	if err == nil || !asError(err, &jiraErr) {
		t.Fatalf("error = %v", err)
	}
	if jiraErr.Attempts != 3 {
		t.Fatalf("attempts = %d", jiraErr.Attempts)
	}
	if requests != 3 {
		t.Fatalf("requests = %d", requests)
	}
	if !reflect.DeepEqual(delays, []time.Duration{100 * time.Millisecond, 150 * time.Millisecond}) {
		t.Fatalf("delays = %#v", delays)
	}
}

func TestRetryAfterAlwaysRespectsZeroDelayBudget(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "3600")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"displayName":"Agent"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 2, BaseDelay: 0, MaxDelay: 0})
	var delays []time.Duration
	client.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	if _, err := client.Myself(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(delays, []time.Duration{0}) {
		t.Fatalf("delays = %#v", delays)
	}
}

func TestSafePostSearchRetries(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/2/search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if requests == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":10,"total":0,"issues":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 2, BaseDelay: 0, MaxDelay: 0})
	client.sleep = func(context.Context, time.Duration) error { return nil }
	result, err := client.Search(context.Background(), SearchOptions{
		JQL:        "project = ENG",
		Fields:     []string{"summary"},
		MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || result.Page.Returned != 0 {
		t.Fatalf("requests = %d, result = %#v", requests, result)
	}
}

func TestMutationIsNeverRetried(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Second})
	if _, err := client.CreateIssue(context.Background(), "ENG", "Task", "One request", ""); err == nil {
		t.Fatal("expected create error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want exactly 1", requests)
	}
}

func TestJiraErrorParsesFieldAndRateLimitDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.Header().Set("RateLimit-Reason", "jira-cost-based")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{
			"errorMessages":["request rejected"],
			"errors":{"summary":"Summary is required"}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Second})
	_, err := client.Myself(context.Background())
	var jiraErr *Error
	if err == nil || !asError(err, &jiraErr) {
		t.Fatalf("error = %v", err)
	}
	if jiraErr.RetryAfter != 3*time.Second || jiraErr.RateLimitReason != "jira-cost-based" {
		t.Fatalf("retry metadata = %#v", jiraErr)
	}
	if jiraErr.FieldErrors["summary"] != "Summary is required" {
		t.Fatalf("field errors = %#v", jiraErr.FieldErrors)
	}
}

func TestJiraErrorRedactsCloudBasicAuthorization(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errorMessages": []string{"rejected " + authorization},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "cloud-token", "agent@example.com", DeploymentCloud, time.Second)
	_, err := client.Myself(context.Background())
	var jiraErr *Error
	if err == nil || !asError(err, &jiraErr) {
		t.Fatalf("error = %v", err)
	}
	if authorization == "" {
		t.Fatal("authorization header was empty")
	}
	if strings.Contains(jiraErr.Error(), authorization) ||
		strings.Contains(jiraErr.Error(), "cloud-token") {
		t.Fatalf("authorization leaked: %s", jiraErr.Error())
	}
}

func asError(err error, target **Error) bool {
	current, ok := err.(*Error)
	if ok {
		*target = current
	}
	return ok
}
