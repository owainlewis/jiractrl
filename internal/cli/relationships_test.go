package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinksListTextMakesDirectionExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"fields":{"issuelinks":[
			{"id":"10","type":{"id":"1","name":"Blocks","inward":"is blocked by","outward":"blocks"},"outwardIssue":{"id":"2","key":"ENG-2"}},
			{"id":"11","type":{"id":"1","name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"id":"3","key":"ENG-3"}}
		]}}`))
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "links", "list", "ENG-1",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	for _, expected := range []string{
		"ENG-1  --outward:blocks-->  ENG-2",
		"ENG-1  --inward:is blocked by-->  ENG-3",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("stdout missing %q: %s", expected, stdout.String())
		}
	}
}

func TestAddLinkJSONExplainsDuplicateSemantics(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "data_center")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "links", "add",
		"--type", "Duplicate", "--outward", "ENG-1", "--inward", "ENG-2", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if requests != 1 ||
		!strings.Contains(stdout.String(), `"duplicateRequestsSucceed": true`) ||
		!strings.Contains(stdout.String(), `"serverReturnsCreatedLinkId": false`) {
		t.Fatalf("requests = %d, stdout = %s", requests, stdout.String())
	}
}

func TestSafeDownloadPathRejectsTraversal(t *testing.T) {
	for _, path := range []string{"../secret", "safe/../../secret", `safe\..\secret`} {
		if _, err := safeDownloadPath(path); err == nil {
			t.Fatalf("path %q was accepted", path)
		}
	}
	for _, path := range []string{"/tmp/evidence.txt", "downloads/evidence.txt", "evidence.txt"} {
		if _, err := safeDownloadPath(path); err != nil {
			t.Fatalf("path %q: %v", path, err)
		}
	}
}

func TestAttachmentDownloadRefusesExistingDestinationBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte("replacement"))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", configPath, "attachments", "download", "10001", "--output", destination,
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error = %v", err)
	}
	data, _ := os.ReadFile(destination)
	if requests != 0 || string(data) != "original" {
		t.Fatalf("requests = %d, destination = %q", requests, data)
	}
}

type fakeAttachmentDownloader struct {
	content   string
	beforeEnd func()
}

func (d fakeAttachmentDownloader) DownloadAttachment(_ context.Context, _ string, destination io.Writer) (int64, error) {
	if _, err := io.WriteString(destination, d.content); err != nil {
		return 0, err
	}
	if d.beforeEnd != nil {
		d.beforeEnd()
	}
	return int64(len(d.content)), nil
}

func TestDownloadAttachmentFileInstallsAtomically(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "evidence.txt")
	written, err := downloadAttachmentFile(context.Background(), fakeAttachmentDownloader{content: "evidence"}, "10001", destination, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || written != 8 || string(data) != "evidence" {
		t.Fatalf("written = %d, data = %q, err = %v", written, data, err)
	}
}

func TestDownloadAttachmentFileDoesNotLoseRaceToOverwrite(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "evidence.txt")
	downloader := fakeAttachmentDownloader{
		content: "download",
		beforeEnd: func() {
			if err := os.WriteFile(destination, []byte("winner"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	_, err := downloadAttachmentFile(context.Background(), downloader, "10001", destination, false)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error = %v", err)
	}
	data, _ := os.ReadFile(destination)
	if string(data) != "winner" {
		t.Fatalf("destination = %q", data)
	}
}

func TestCollectChangelogAdvancesAcrossFilteredEmptyPage(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Query().Get("startAt") {
		case "0":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":1,"total":2,"values":[{"id":"1","items":[{"field":"status"}]}]}`))
		case "1":
			_, _ = w.Write([]byte(`{"startAt":1,"maxResults":1,"total":2,"isLast":true,"values":[{"id":"2","items":[{"field":"priority","fromString":"Low","toString":"High"}]}]}`))
		default:
			t.Fatalf("startAt = %q", r.URL.Query().Get("startAt"))
		}
	}))
	defer server.Close()

	configPath := writeCommentsConfig(t, server.URL, "cloud")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"--config", configPath, "changelog", "ENG-1",
		"--field", "priority", "--max", "1", "--all", "--limit", "2", "--json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	if requests != 2 || !strings.Contains(stdout.String(), `"id": "2"`) ||
		!strings.Contains(stdout.String(), `"scanned": 2`) ||
		!strings.Contains(stdout.String(), `"hasMore": false`) {
		t.Fatalf("requests = %d, stdout = %s", requests, stdout.String())
	}
}
