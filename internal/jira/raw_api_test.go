package jira

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRawAPISameOriginJSONUsesConfiguredAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/example/1" ||
			r.URL.Query().Get("expand") != "all" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer secret" ||
			r.Header.Get("X-Trace-ID") != "trace-1" {
			t.Fatalf("headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Set-Cookie", "session=private")
		_, _ = w.Write([]byte(`{"ok":true,"count":2}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "", DeploymentDataCenter, time.Second)
	result, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet,
		Path:   "/rest/example/1?expand=all",
		Headers: map[string]string{
			"X-Trace-ID": "trace-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := result.Body.(map[string]any)
	if result.Status != http.StatusOK || !result.JSON || !ok ||
		body["count"] != json.Number("2") ||
		result.Headers["Etag"][0] != `"v1"` ||
		result.Headers["Set-Cookie"] != nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestRawAPIAutoDetectionUsesCloudBasicAuthentication(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			if r.URL.Path != "/rest/api/2/serverInfo" || r.Header.Get("Authorization") != "Bearer secret" {
				t.Fatalf("first request = %s, auth = %q", r.URL.Path, r.Header.Get("Authorization"))
			}
			http.Error(w, "use basic authentication", http.StatusUnauthorized)
		case 2:
			username, password, ok := r.BasicAuth()
			if r.URL.Path != "/rest/api/2/serverInfo" || !ok ||
				username != "user@example.com" || password != "secret" {
				t.Fatalf("second request = %s, auth = %q", r.URL.Path, r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deploymentType":"Cloud"}`))
		case 3:
			username, password, ok := r.BasicAuth()
			if r.URL.Path != "/rest/custom/1" || !ok ||
				username != "user@example.com" || password != "secret" {
				t.Fatalf("raw request = %s, auth = %q", r.URL.Path, r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", "user@example.com", DeploymentAuto, time.Second)
	if _, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/custom/1",
	}); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestRawAPIAutoDetectionDoesNotFollowRedirects(t *testing.T) {
	redirectedRequests := 0
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedRequests++
	}))
	defer redirected.Close()

	originRequests := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests++
		if r.URL.Path != "/rest/api/2/serverInfo" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		http.Redirect(w, r, redirected.URL+"/stolen", http.StatusFound)
	}))
	defer origin.Close()

	client := NewClient(origin.URL, "secret", "", DeploymentAuto, time.Second)
	_, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/custom/1",
	})
	var jiraErr *Error
	if !errors.As(err, &jiraErr) || jiraErr.StatusCode != http.StatusFound ||
		originRequests != 1 || redirectedRequests != 0 {
		t.Fatalf("error = %v, origin = %d, redirected = %d", err, originRequests, redirectedRequests)
	}
}

func TestRawAPIPreservesNonJSONBytesAndContentType(t *testing.T) {
	payload := []byte{0x00, 0xff, 0x10, 'A'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/binary",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(result.BodyBase64)
	if err != nil {
		t.Fatal(err)
	}
	if result.JSON || result.ContentType != "application/octet-stream" ||
		result.Bytes != len(payload) || !bytes.Equal(decoded, payload) {
		t.Fatalf("result = %#v, decoded = %v", result, decoded)
	}
}

func TestRawAPIPreservesCompressedResponseBytes(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("compressed payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte(nil), compressed.Bytes()...)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Fatalf("accept encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	result, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/compressed",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(result.BodyBase64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, payload) || result.Headers["Content-Encoding"][0] != "gzip" {
		t.Fatalf("result = %#v, decoded bytes = %d", result, len(decoded))
	}
}

func TestRawAPIKeepsConfiguredBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jira/rest/api/2/myself" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL+"/jira", "token", "", DeploymentDataCenter, time.Second)
	if _, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/api/2/myself",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRawAPIRejectsUnsafePathsBeforeNetwork(t *testing.T) {
	paths := []string{
		"https://evil.example/rest/api/3/myself",
		"//evil.example/rest/api/3/myself",
		"rest/api/3/myself",
		"/../admin",
		"/%2e%2e/admin",
		"/%252e%252e/admin",
		"/%252525252e%252525252e/admin",
		"/..;/admin",
		"/%252e%252e%253b/admin",
		"/./rest/api/3/myself",
		"/%2f%2fevil.example/admin",
		"/%252f%252fevil.example/admin",
		"/%252525252f%252525252fevil.example/admin",
		`/rest\..\admin`,
		"/rest/api/3/myself#fragment",
		"/rest/api/3/%2523fragment",
		"/rest/api/3/%250aheader",
		"/rest/api/3/myself?_method=DELETE",
		"/rest/api/3/myself?%255fmethod=DELETE",
		" /rest/api/3/myself",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
			}))
			defer server.Close()
			client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
			_, err := client.RawAPI(context.Background(), RawAPIRequest{
				Method: http.MethodGet, Path: path,
			})
			if err == nil || requests != 0 {
				t.Fatalf("error = %v, requests = %d", err, requests)
			}
		})
	}
}

func TestRawAPIRejectsProtectedHeadersBeforeNetwork(t *testing.T) {
	headers := []map[string]string{
		{"Authorization": "Bearer attacker"},
		{"Proxy-Authorization": "secret"},
		{"Host": "evil.example"},
		{"Cookie": "session=secret"},
		{"Content-Length": "0"},
		{"X-Forwarded-Host": "evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
		{"X-Atlassian-Token": "no-check"},
		{"X-HTTP-Method-Override": "DELETE"},
		{"X-HTTP-Method": "POST"},
		{"X-Method-Override": "PATCH"},
		{"X-Original-Method": "DELETE"},
		{"X-Original-URL": "https://evil.example/admin"},
		{"X-Rewrite-URL": "/admin"},
		{"Proxy": "evil.example"},
		{"Bad Header": "value"},
		{"X-Test": "value\r\nAuthorization: attacker"},
		{"X-Test": "value\x01"},
	}
	for _, header := range headers {
		t.Run(firstHeaderName(header), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
			}))
			defer server.Close()
			client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
			_, err := client.RawAPI(context.Background(), RawAPIRequest{
				Method: http.MethodGet, Path: "/rest/api/2/myself", Headers: header,
			})
			if err == nil || requests != 0 {
				t.Fatalf("error = %v, requests = %d", err, requests)
			}
		})
	}
}

func TestRawAPIDoesNotFollowRedirects(t *testing.T) {
	redirectedRequests := 0
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedRequests++
	}))
	defer redirected.Close()

	originRequests := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests++
		http.Redirect(w, r, redirected.URL+"/stolen", http.StatusFound)
	}))
	defer origin.Close()

	client := NewClient(origin.URL, "secret", "", DeploymentDataCenter, time.Second)
	_, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/redirect",
	})
	var jiraErr *Error
	if !errors.As(err, &jiraErr) || jiraErr.StatusCode != http.StatusFound ||
		originRequests != 1 || redirectedRequests != 0 {
		t.Fatalf("error = %v, origin = %d, redirected = %d", err, originRequests, redirectedRequests)
	}
}

func TestRawAPIDoesNotFollowSameOriginRedirect(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/next", http.StatusTemporaryRedirect)
			return
		}
		t.Fatal("redirect was followed")
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	_, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/start",
	})
	var jiraErr *Error
	if !errors.As(err, &jiraErr) || jiraErr.StatusCode != http.StatusTemporaryRedirect || requests != 1 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
}

func TestRawAPIReadRetriesButWriteDoesNot(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if requests == 1 {
				http.Error(w, "temporary", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()
		client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
		client.SetRetryPolicy(RetryPolicy{MaxAttempts: 2})
		client.sleep = func(context.Context, time.Duration) error { return nil }
		_, err := client.RawAPI(context.Background(), RawAPIRequest{
			Method: http.MethodGet, Path: "/rest/read",
		})
		if err != nil || requests != 2 {
			t.Fatalf("error = %v, requests = %d", err, requests)
		}
	})

	t.Run("write", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			http.Error(w, "temporary", http.StatusServiceUnavailable)
		}))
		defer server.Close()
		client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
		client.SetRetryPolicy(RetryPolicy{MaxAttempts: 3})
		_, err := client.RawAPI(context.Background(), RawAPIRequest{
			Method: http.MethodPost, Path: "/rest/write", Body: []byte(`{"value":1}`), AllowWrite: true,
		})
		if err == nil || requests != 1 {
			t.Fatalf("error = %v, requests = %d", err, requests)
		}
	})
}

func TestRawAPIUsesHTTPClientTimeoutWithoutRetryingTransportFailure(t *testing.T) {
	requests := 0
	client := NewClient("https://jira.example.com", "token", "", DeploymentDataCenter, 10*time.Millisecond)
	client.http.Transport = rawAPIRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 3})

	_, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/slow",
	})
	if err == nil || requests != 1 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
}

func TestRawAPIWriteRequiresConfirmationBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)

	_, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodPatch, Path: "/rest/write", Body: []byte(`{"value":1}`),
	})
	if err == nil || requests != 0 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
}

func TestRawAPIRequestLimitsAndContentTypeFailBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)

	inputs := []RawAPIRequest{
		{
			Method: http.MethodGet, Path: "/" + strings.Repeat("a", maxRawAPIPathBytes),
		},
		{
			Method: http.MethodPost, Path: "/rest/write",
			Body: append(bytes.Repeat([]byte(" "), MaxRawAPIRequestBytes), 'x'), AllowWrite: true,
		},
		{
			Method: http.MethodPost, Path: "/rest/write", Body: []byte(`{"value":1}`),
			Headers: map[string]string{"Content-Type": "text/plain"}, AllowWrite: true,
		},
	}
	for _, input := range inputs {
		if _, err := client.RawAPI(context.Background(), input); err == nil {
			t.Fatalf("input = %#v returned nil", input)
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestRawAPIWritePreservesExactJSONBody(t *testing.T) {
	body := []byte("{\n  \"value\": 1,\n  \"items\": [\"a\", \"b\"]\n}\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPatch || r.Header.Get("Content-Type") != "application/json" ||
			!bytes.Equal(got, body) {
			t.Fatalf("method = %s, content type = %q, body = %q", r.Method, r.Header.Get("Content-Type"), got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)

	result, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodPatch, Path: "/rest/write", Body: body, AllowWrite: true,
	})
	if err != nil || result.Status != http.StatusNoContent {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestRawAPIErrorRedactsConfiguredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["token secret-token was rejected"]}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "secret-token", "", DeploymentDataCenter, time.Second)

	_, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/error",
	})
	var jiraErr *Error
	if !errors.As(err, &jiraErr) || strings.Contains(err.Error(), "secret-token") ||
		!reflect.DeepEqual(jiraErr.ErrorMessages, []string{"token [REDACTED] was rejected"}) {
		t.Fatalf("error = %#v", err)
	}
}

func TestRawAPIRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(bytes.Repeat([]byte("x"), MaxRawAPIResponseBytes+1))
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, 5*time.Second)

	_, err := client.RawAPI(context.Background(), RawAPIRequest{
		Method: http.MethodGet, Path: "/rest/large",
	})
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func firstHeaderName(header map[string]string) string {
	for name := range header {
		return name
	}
	return "header"
}

type rawAPIRoundTripper func(*http.Request) (*http.Response, error)

func (fn rawAPIRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
