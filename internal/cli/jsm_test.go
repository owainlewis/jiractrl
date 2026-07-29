package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSMReadCommandsReturnLosslessJSON(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		path    string
	}{
		{name: "service desks", command: []string{"jsm", "service-desks", "list", "--start", "2", "--max", "3", "--json"}, path: "/rest/servicedeskapi/servicedesk"},
		{name: "service desk", command: []string{"jsm", "service-desks", "get", "7", "--json"}, path: "/rest/servicedeskapi/servicedesk/7"},
		{name: "queues", command: []string{"jsm", "queues", "list", "7", "--include-count", "--json"}, path: "/rest/servicedeskapi/servicedesk/7/queue"},
		{name: "request types", command: []string{"jsm", "request-types", "list", "7", "--json"}, path: "/rest/servicedeskapi/servicedesk/7/requesttype"},
		{name: "request fields", command: []string{"jsm", "request-types", "fields", "--service-desk", "7", "--request-type", "9", "--json"}, path: "/rest/servicedeskapi/servicedesk/7/requesttype/9/field"},
		{name: "requests", command: []string{"jsm", "requests", "list", "--service-desk", "7", "--search", "laptop", "--json"}, path: "/rest/servicedeskapi/request"},
		{name: "request", command: []string{"jsm", "requests", "get", "HELP-1", "--json"}, path: "/rest/servicedeskapi/request/HELP-1"},
		{name: "comments", command: []string{"jsm", "comments", "list", "HELP-1", "--json"}, path: "/rest/servicedeskapi/request/HELP-1/comment"},
		{name: "participants", command: []string{"jsm", "participants", "list", "HELP-1", "--json"}, path: "/rest/servicedeskapi/request/HELP-1/participant"},
		{name: "SLAs", command: []string{"jsm", "slas", "list", "HELP-1", "--json"}, path: "/rest/servicedeskapi/request/HELP-1/sla"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					if r.URL.Path != "/rest/servicedeskapi/servicedesk" ||
						r.URL.Query().Get("limit") != "1" {
						t.Fatalf("capability request = %s", r.URL.String())
					}
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				if r.URL.Path != test.path {
					t.Fatalf("path = %q, want %q", r.URL.Path, test.path)
				}
				if strings.Contains(test.name, "request") && !strings.Contains(test.name, "fields") &&
					(r.URL.Path == "/rest/servicedeskapi/request" || r.URL.Path == "/rest/servicedeskapi/request/HELP-1") &&
					!strings.Contains(r.URL.Query().Get("expand"), "sla") {
					t.Fatalf("expand = %q", r.URL.Query().Get("expand"))
				}
				_, _ = w.Write([]byte(`{
					"values":[{"id":"1","vendor":{"precise":9007199254740993}}],
					"sla":{"ongoingCycle":{"remainingTime":{"millis":1234}}}
				}`))
			}))
			defer server.Close()

			args := append([]string{"--config", writeCommentsConfig(t, server.URL, "data_center")}, test.command...)
			var stdout, stderr bytes.Buffer
			if err := Run(args, &stdout, &stderr); err != nil {
				t.Fatalf("error = %v, stderr = %s", err, stderr.String())
			}
			var envelope struct {
				OK   bool            `json:"ok"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if !envelope.OK || !bytes.Contains(envelope.Data, []byte(`9007199254740993`)) ||
				!bytes.Contains(envelope.Data, []byte(`"sla"`)) || requests != 2 {
				t.Fatalf("stdout = %s, requests = %d", stdout.String(), requests)
			}
		})
	}
}

func TestJSMRequestCreateDiscoversAndValidatesFields(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "request-fields.json")
	if err := os.WriteFile(inputPath, []byte(`{
		"summary":"Need a laptop",
		"customfield_10001":9007199254740993
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/servicedeskapi/servicedesk":
			_, _ = w.Write([]byte(`{"values":[]}`))
		case strings.HasSuffix(r.URL.Path, "/field"):
			_, _ = w.Write([]byte(`{
				"requestTypeFields":[
					{"fieldId":"summary","name":"Summary","required":true},
					{"fieldId":"customfield_10001","name":"Asset","required":true}
				]
			}`))
		case r.URL.Path == "/rest/servicedeskapi/request":
			writes++
			var payload struct {
				ServiceDeskID      string                     `json:"serviceDeskId"`
				RequestTypeID      string                     `json:"requestTypeId"`
				RequestFieldValues map[string]json.RawMessage `json:"requestFieldValues"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ServiceDeskID != "7" || payload.RequestTypeID != "9" ||
				string(payload.RequestFieldValues["customfield_10001"]) != "9007199254740993" {
				t.Fatalf("payload = %#v", payload)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"issueKey":"HELP-1","requestTypeId":"9"}`))
		default:
			t.Fatalf("unexpected request = %s", r.URL.String())
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"jsm", "requests", "create", "--service-desk", "7", "--request-type", "9",
		"--input", inputPath, "--json",
	}, &stdout, &stderr)
	if err != nil || writes != 1 || !strings.Contains(stdout.String(), `"issueKey": "HELP-1"`) {
		t.Fatalf("error = %v, writes = %d, stdout = %s, stderr = %s", err, writes, stdout.String(), stderr.String())
	}
}

func TestJSMRequestCreateRequiredFieldFailurePerformsNoWrite(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "request-fields.json")
	if err := os.WriteFile(inputPath, []byte(`{"description":"missing summary"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		if strings.HasSuffix(r.URL.Path, "/field") {
			_, _ = w.Write([]byte(`{
				"requestTypeFields":[
					{"fieldId":"summary","name":"Summary","required":true},
					{"fieldId":"description","name":"Description","required":false}
				]
			}`))
			return
		}
		_, _ = w.Write([]byte(`{"values":[]}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run([]string{
		"--config", writeCommentsConfig(t, server.URL, "data_center"),
		"jsm", "requests", "create", "--service-desk", "7", "--request-type", "9",
		"--input", inputPath, "--json",
	}, &stdout, &stderr)
	if err == nil || !IsReported(err) || writes != 0 ||
		!strings.Contains(stderr.String(), `"requestFieldValues.summary"`) {
		t.Fatalf("error = %v, writes = %d, stderr = %s", err, writes, stderr.String())
	}
}

func TestJSMCommentVisibilityMustBeExplicitAndIsReported(t *testing.T) {
	t.Run("missing visibility", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
		}))
		defer server.Close()
		var stdout, stderr bytes.Buffer
		err := Run([]string{
			"--config", writeCommentsConfig(t, server.URL, "data_center"),
			"jsm", "comments", "add", "HELP-1", "--body", "Hello", "--json",
		}, &stdout, &stderr)
		if err == nil || !IsReported(err) || requests != 0 {
			t.Fatalf("error = %v, requests = %d", err, requests)
		}
	})

	for _, visibility := range []string{"public", "internal"} {
		t.Run(visibility, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/servicedeskapi/servicedesk" {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				writes++
				var payload struct {
					Body   string `json:"body"`
					Public bool   `json:"public"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload.Body != "Hello" || payload.Public != (visibility == "public") {
					t.Fatalf("payload = %#v", payload)
				}
				w.WriteHeader(http.StatusCreated)
				responseBody := `{"id":"1","body":"Hello","public":false}`
				if visibility == "public" {
					responseBody = `{"id":"1","body":"Hello","public":true}`
				}
				_, _ = w.Write([]byte(responseBody))
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			err := Run([]string{
				"--config", writeCommentsConfig(t, server.URL, "data_center"),
				"jsm", "comments", "add", "HELP-1", "--body", "Hello",
				"--visibility", visibility, "--json",
			}, &stdout, &stderr)
			if err != nil || writes != 1 ||
				!strings.Contains(stdout.String(), `"visibility": "`+visibility+`"`) {
				t.Fatalf("error = %v, writes = %d, stdout = %s", err, writes, stdout.String())
			}
		})
	}
}

func TestJSMParticipantCommandsUseDeploymentIdentity(t *testing.T) {
	tests := []struct {
		name       string
		deployment string
		operation  string
		flag       string
		value      string
		payloadKey string
		method     string
	}{
		{
			name: "cloud add", deployment: "cloud", operation: "add",
			flag: "--account-id", value: "cloud-1", payloadKey: "accountIds", method: http.MethodPost,
		},
		{
			name: "data center remove", deployment: "data_center", operation: "remove",
			flag: "--username", value: "fred", payloadKey: "usernames", method: http.MethodDelete,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/servicedeskapi/servicedesk" {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				if r.Method != test.method {
					t.Fatalf("method = %s", r.Method)
				}
				var payload map[string][]string
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 1 || len(payload[test.payloadKey]) != 1 ||
					payload[test.payloadKey][0] != test.value {
					t.Fatalf("payload = %#v", payload)
				}
				_, _ = w.Write([]byte(`{"values":[]}`))
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			err := Run([]string{
				"--config", writeCommentsConfig(t, server.URL, test.deployment),
				"jsm", "participants", test.operation, "HELP-1",
				test.flag, test.value, "--json",
			}, &stdout, &stderr)
			if err != nil {
				t.Fatalf("error = %v, stderr = %s", err, stderr.String())
			}
		})
	}
}

func TestJSMCapabilityAndPermissionErrorsAreStructured(t *testing.T) {
	tests := []struct {
		name       string
		capability int
		request    int
		kind       string
	}{
		{name: "missing product", capability: http.StatusNotFound, kind: "unsupported"},
		{name: "capability permission", capability: http.StatusForbidden, kind: "permission"},
		{name: "request permission", capability: http.StatusOK, request: http.StatusForbidden, kind: "permission"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/servicedeskapi/servicedesk" &&
					r.URL.Query().Get("limit") == "1" {
					w.WriteHeader(test.capability)
					_, _ = w.Write([]byte(`{"errorMessages":["product or permission"]}`))
					return
				}
				w.WriteHeader(test.request)
				_, _ = w.Write([]byte(`{"errorMessages":["agent permission required"]}`))
			}))
			defer server.Close()

			var stdout, stderr bytes.Buffer
			err := Run([]string{
				"--config", writeCommentsConfig(t, server.URL, "data_center"),
				"jsm", "requests", "get", "HELP-1", "--json",
			}, &stdout, &stderr)
			if err == nil || !IsReported(err) || !strings.Contains(stderr.String(), `"kind": "`+test.kind+`"`) {
				t.Fatalf("error = %v, stderr = %s", err, stderr.String())
			}
		})
	}
}
