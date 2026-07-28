package jira

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIssueAttachmentsAcceptNumericIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/ENG-1" || r.URL.Query().Get("fields") != "attachment" {
			t.Fatalf("request = %s", r.URL.RequestURI())
		}
		_, _ = w.Write([]byte(`{"fields":{"attachment":[{"id":10001,"filename":"evidence.txt","mimeType":"text/plain","size":12}]}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	attachments, err := client.IssueAttachments(context.Background(), "ENG-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 || attachments[0].ID != "10001" || attachments[0].Filename != "evidence.txt" {
		t.Fatalf("attachments = %#v", attachments)
	}
}

func TestUploadAttachmentStreamsRequiredMultipartForm(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/2/issue/ENG-1/attachments" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Atlassian-Token") != "no-check" {
			t.Fatalf("X-Atlassian-Token = %q", r.Header.Get("X-Atlassian-Token"))
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("content type = %q, err = %v", r.Header.Get("Content-Type"), err)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() != "file" || part.FileName() != "evidence.txt" {
			t.Fatalf("part = %q %q", part.FormName(), part.FileName())
		}
		data, err := io.ReadAll(part)
		if err != nil || string(data) != "streamed evidence" {
			t.Fatalf("data = %q, err = %v", data, err)
		}
		if next, err := reader.NextPart(); err != io.EOF || next != nil {
			t.Fatalf("extra part = %#v, err = %v", next, err)
		}
		_, _ = w.Write([]byte(`{"id":"10001","filename":"evidence.txt","size":17}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	attachments, err := client.UploadAttachment(context.Background(), "ENG-1", "/tmp/evidence.txt", strings.NewReader("streamed evidence"))
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(attachments) != 1 || attachments[0].ID != "10001" {
		t.Fatalf("requests = %d, attachments = %#v", requests, attachments)
	}
}

func TestDownloadAttachmentCloudAvoidsRedirectAndStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/attachment/content/10001" || r.URL.Query().Get("redirect") != "false" {
			t.Fatalf("request = %s", r.URL.RequestURI())
		}
		_, _ = w.Write([]byte("downloaded bytes"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "agent@example.com", DeploymentCloud, time.Second)
	var destination bytes.Buffer
	written, err := client.DownloadAttachment(context.Background(), "10001", &destination)
	if err != nil {
		t.Fatal(err)
	}
	if written != 16 || destination.String() != "downloaded bytes" {
		t.Fatalf("written = %d, body = %q", written, destination.String())
	}
}

func TestDownloadAttachmentDataCenterRequiresSameOriginContent(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/attachment/10001":
			_, _ = w.Write([]byte(`{"id":"10001","content":"` + server.URL + `/secure/attachment/10001/evidence.txt"}`))
		case "/secure/attachment/10001/evidence.txt":
			_, _ = w.Write([]byte("server bytes"))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	var destination bytes.Buffer
	if _, err := client.DownloadAttachment(context.Background(), "10001", &destination); err != nil {
		t.Fatal(err)
	}
	if destination.String() != "server bytes" {
		t.Fatalf("body = %q", destination.String())
	}
}

func TestDownloadAttachmentRejectsCrossOriginContent(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"id":"10001","content":"https://evil.example/file"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	_, err := client.DownloadAttachment(context.Background(), "10001", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "configured Jira origin") {
		t.Fatalf("error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestDownloadAttachmentRejectsCrossOriginRedirect(t *testing.T) {
	crossOriginRequests := 0
	crossOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossOriginRequests++
		_, _ = w.Write([]byte("should not be fetched"))
	}))
	defer crossOrigin.Close()

	var jira *httptest.Server
	jira = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/attachment/10001":
			_, _ = w.Write([]byte(`{"id":"10001","content":"` + jira.URL + `/secure/attachment/10001/evidence.txt"}`))
		case "/secure/attachment/10001/evidence.txt":
			http.Redirect(w, r, crossOrigin.URL+"/stolen", http.StatusFound)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer jira.Close()

	client := NewClient(jira.URL, "secret-token", "", DeploymentDataCenter, time.Second)
	_, err := client.DownloadAttachment(context.Background(), "10001", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "redirect target") {
		t.Fatalf("error = %v", err)
	}
	if crossOriginRequests != 0 {
		t.Fatalf("cross-origin requests = %d, want 0", crossOriginRequests)
	}
}

func TestRemoveAttachmentIsNeverRetried(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentCloud, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})
	if err := client.RemoveAttachment(context.Background(), "10001"); err == nil {
		t.Fatal("expected error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
