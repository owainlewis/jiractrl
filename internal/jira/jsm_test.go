package jira

import (
	"bytes"
	"context"
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

func TestServiceManagementCapabilityDetection(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		want        CapabilityStatus
		unsupported bool
		permission  bool
	}{
		{name: "available", status: http.StatusOK, want: CapabilityAvailable},
		{name: "missing", status: http.StatusNotFound, want: CapabilityMissing, unsupported: true},
		{name: "permission", status: http.StatusForbidden, want: CapabilityUnknown, permission: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/rest/servicedeskapi/servicedesk" ||
					r.URL.Query().Get("limit") != "1" {
					t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"errorMessages":["JSM access denied"]}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
			status, err := client.ServiceManagementCapability(context.Background())
			var permission *PermissionError
			if status != test.want || errors.As(err, &permission) != test.permission {
				t.Fatalf("status = %q, error = %v", status, err)
			}
			requireErr := client.RequireServiceManagement(context.Background())
			var unsupported *UnsupportedCapabilityError
			if errors.As(requireErr, &unsupported) != test.unsupported ||
				errors.As(requireErr, &permission) != test.permission {
				t.Fatalf("require error = %v", requireErr)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1 cached capability probe", requests)
			}
		})
	}
}

func TestJSMReadEndpointsUseBoundedPaginationAndPreserveJSON(t *testing.T) {
	type readCase struct {
		name      string
		path      string
		query     map[string]string
		call      func(*Client) (json.RawMessage, error)
		wantToken string
	}
	cases := []readCase{
		{
			name: "service desks", path: "/rest/servicedeskapi/servicedesk",
			query: map[string]string{"start": "3", "limit": "2"},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMServiceDesks(context.Background(), 3, 2)
			},
		},
		{
			name: "service desk", path: "/rest/servicedeskapi/servicedesk/7",
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMServiceDesk(context.Background(), "7")
			},
		},
		{
			name: "queues", path: "/rest/servicedeskapi/servicedesk/7/queue",
			query: map[string]string{"start": "4", "limit": "5", "includeCount": "true"},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMQueues(context.Background(), "7", 4, 5, true)
			},
		},
		{
			name: "request types", path: "/rest/servicedeskapi/servicedesk/7/requesttype",
			query: map[string]string{"start": "1", "limit": "6"},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMRequestTypes(context.Background(), "7", 1, 6)
			},
		},
		{
			name: "request type fields", path: "/rest/servicedeskapi/servicedesk/7/requesttype/9/field",
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMRequestTypeFields(context.Background(), "7", "9")
			},
		},
		{
			name: "request search", path: "/rest/servicedeskapi/request",
			query: map[string]string{
				"start": "2", "limit": "7", "serviceDeskId": "7", "requestTypeId": "9",
				"requestStatus": "OPEN_REQUESTS", "requestOwnership": "OWNED_REQUESTS",
				"searchTerm": "laptop", "expand": defaultJSMRequestExpand,
			},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMRequests(context.Background(), JSMRequestOptions{
					Start: 2, Limit: 7, ServiceDeskID: "7", RequestTypeID: "9",
					RequestStatus: "OPEN_REQUESTS", RequestOwnership: "OWNED_REQUESTS",
					SearchTerm: "laptop",
				})
			},
			wantToken: `"sla"`,
		},
		{
			name: "request", path: "/rest/servicedeskapi/request/HELP-1",
			query: map[string]string{"expand": defaultJSMRequestExpand},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMRequest(context.Background(), "HELP-1", "")
			},
			wantToken: `"sla"`,
		},
		{
			name: "comments", path: "/rest/servicedeskapi/request/HELP-1/comment",
			query: map[string]string{"start": "2", "limit": "8"},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMRequestComments(context.Background(), "HELP-1", 2, 8)
			},
		},
		{
			name: "participants", path: "/rest/servicedeskapi/request/HELP-1/participant",
			query: map[string]string{"start": "2", "limit": "8"},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMRequestParticipants(context.Background(), "HELP-1", 2, 8)
			},
		},
		{
			name: "SLAs", path: "/rest/servicedeskapi/request/HELP-1/sla",
			query: map[string]string{"start": "2", "limit": "8"},
			call: func(client *Client) (json.RawMessage, error) {
				return client.JSMRequestSLAs(context.Background(), "HELP-1", 2, 8)
			},
			wantToken: `"sla"`,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					if r.URL.Path != "/rest/servicedeskapi/servicedesk" ||
						r.URL.Query().Get("limit") != "1" {
						t.Fatalf("capability request = %s?%s", r.URL.Path, r.URL.RawQuery)
					}
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				if r.URL.Path != test.path {
					t.Fatalf("path = %q, want %q", r.URL.Path, test.path)
				}
				for name, want := range test.query {
					if got := r.URL.Query().Get(name); got != want {
						t.Fatalf("query %s = %q, want %q (%s)", name, got, want, r.URL.RawQuery)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"size":1,
					"isLastPage":true,
					"values":[{"vendorField":{"precise":9007199254740993}}],
					"sla":{"ongoingCycle":{"breached":false}}
				}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
			result, err := test.call(client)
			if err != nil {
				t.Fatal(err)
			}
			if requests != 2 || !bytes.Contains(result, []byte(`9007199254740993`)) {
				t.Fatalf("requests = %d, result = %s", requests, result)
			}
			if test.wantToken != "" && !strings.Contains(string(result), test.wantToken) {
				t.Fatalf("result = %s, want %s", result, test.wantToken)
			}
		})
	}
}

func TestCreateJSMRequestValidatesDiscoveredRequiredFields(t *testing.T) {
	tests := []struct {
		name       string
		fields     map[string]any
		wantErr    string
		wantWrites int
	}{
		{
			name: "valid typed values",
			fields: map[string]any{
				"summary":           "Need a laptop",
				"customfield_10001": json.Number("9007199254740993"),
			},
			wantWrites: 1,
		},
		{name: "missing required", fields: map[string]any{"customfield_10001": "1"}, wantErr: "summary"},
		{name: "empty required", fields: map[string]any{"summary": " ", "customfield_10001": "1"}, wantErr: "summary"},
		{name: "unknown field", fields: map[string]any{"summary": "Help", "customfield_10001": "1", "fieldz": true}, wantErr: "fieldz"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rest/servicedeskapi/servicedesk" && r.URL.Query().Get("limit") == "1":
					_, _ = w.Write([]byte(`{"values":[]}`))
				case r.URL.Path == "/rest/servicedeskapi/servicedesk/7/requesttype/9/field":
					_, _ = w.Write([]byte(`{
						"requestTypeFields":[
							{"fieldId":"summary","name":"Summary","required":true},
							{"fieldId":"customfield_10001","name":"Asset","required":false}
						],
						"canRaiseOnBehalfOf":true,
						"canAddRequestParticipants":true
					}`))
				case r.URL.Path == "/rest/servicedeskapi/request":
					writes++
					if r.Method != http.MethodPost {
						t.Fatalf("method = %s", r.Method)
					}
					var body map[string]any
					decoder := json.NewDecoder(r.Body)
					decoder.UseNumber()
					if err := decoder.Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["serviceDeskId"] != "7" || body["requestTypeId"] != "9" ||
						!reflect.DeepEqual(body["requestFieldValues"], test.fields) {
						t.Fatalf("body = %#v", body)
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"issueKey":"HELP-1","vendor":{"kept":true}}`))
				default:
					t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
			result, err := client.CreateJSMRequest(context.Background(), "7", "9", test.fields)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) || writes != 0 {
					t.Fatalf("result = %s, error = %v, writes = %d", result, err, writes)
				}
				return
			}
			if err != nil || writes != test.wantWrites || !strings.Contains(string(result), `"vendor"`) {
				t.Fatalf("result = %s, error = %v, writes = %d", result, err, writes)
			}
		})
	}
}

func TestJSMCommentVisibilityIsExplicitAndWritesAreOneShot(t *testing.T) {
	t.Run("missing visibility", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
		}))
		defer server.Close()
		client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
		if _, err := client.AddJSMRequestComment(context.Background(), "HELP-1", "body", nil); err == nil || requests != 0 {
			t.Fatalf("error = %v, requests = %d", err, requests)
		}
	})

	for _, public := range []bool{true, false} {
		name := "internal"
		if public {
			name = "public"
		}
		t.Run(name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/servicedeskapi/servicedesk" {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				writes++
				var body struct {
					Body   string `json:"body"`
					Public bool   `json:"public"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Body != "Visible body" || body.Public != public {
					t.Fatalf("body = %#v", body)
				}
				http.Error(w, "temporary", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
			client.SetRetryPolicy(RetryPolicy{MaxAttempts: 3})
			_, err := client.AddJSMRequestComment(context.Background(), "HELP-1", "Visible body", &public)
			if err == nil || writes != 1 {
				t.Fatalf("error = %v, writes = %d", err, writes)
			}
		})
	}

	t.Run("response mismatch", func(t *testing.T) {
		writes := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/rest/servicedeskapi/servicedesk" {
				_, _ = w.Write([]byte(`{"values":[]}`))
				return
			}
			writes++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"1","public":false}`))
		}))
		defer server.Close()
		client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
		public := true
		_, err := client.AddJSMRequestComment(context.Background(), "HELP-1", "body", &public)
		if err == nil || !strings.Contains(err.Error(), "visibility") || writes != 1 {
			t.Fatalf("error = %v, writes = %d", err, writes)
		}
	})
}

func TestJSMParticipantIdentityMatchesDeployment(t *testing.T) {
	tests := []struct {
		name       string
		deployment Deployment
		method     string
		input      JSMParticipantInput
		payloadKey string
		payload    []string
	}{
		{
			name: "cloud account IDs", deployment: DeploymentCloud, method: http.MethodPost,
			input:      JSMParticipantInput{AccountIDs: []string{"cloud-1,cloud-2"}},
			payloadKey: "accountIds", payload: []string{"cloud-1", "cloud-2"},
		},
		{
			name: "data center usernames", deployment: DeploymentDataCenter, method: http.MethodDelete,
			input:      JSMParticipantInput{Usernames: []string{"fred,mary"}},
			payloadKey: "usernames", payload: []string{"fred", "mary"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.deployment == DeploymentCloud {
					username, password, ok := r.BasicAuth()
					if !ok || username != "cloud@example.com" || password != "token" {
						t.Fatalf("cloud auth = %q", r.Header.Get("Authorization"))
					}
				} else if r.Header.Get("Authorization") != "Bearer token" {
					t.Fatalf("data center auth = %q", r.Header.Get("Authorization"))
				}
				if r.URL.Path == "/rest/servicedeskapi/servicedesk" {
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				writes++
				if r.Method != test.method || r.URL.Path != "/rest/servicedeskapi/request/HELP-1/participant" {
					t.Fatalf("request = %s %s", r.Method, r.URL.Path)
				}
				var payload map[string][]string
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 1 || !reflect.DeepEqual(payload[test.payloadKey], test.payload) {
					t.Fatalf("payload = %#v", payload)
				}
				_, _ = w.Write([]byte(`{"values":[]}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "cloud@example.com", test.deployment, time.Second)
			result, err := client.ChangeJSMRequestParticipants(
				context.Background(), "HELP-1", test.input, test.method == http.MethodDelete,
			)
			if err != nil || writes != 1 || len(result) == 0 {
				t.Fatalf("result = %s, error = %v, writes = %d", result, err, writes)
			}
		})
	}
}

func TestJSMWrongParticipantIdentityPerformsNoMutation(t *testing.T) {
	tests := []struct {
		deployment Deployment
		input      JSMParticipantInput
	}{
		{deployment: DeploymentCloud, input: JSMParticipantInput{Usernames: []string{"fred"}}},
		{deployment: DeploymentDataCenter, input: JSMParticipantInput{AccountIDs: []string{"cloud-1"}}},
	}
	for _, test := range tests {
		t.Run(string(test.deployment), func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
				}
				_, _ = w.Write([]byte(`{"values":[]}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "", test.deployment, time.Second)
			_, err := client.ChangeJSMRequestParticipants(context.Background(), "HELP-1", test.input, false)
			if err == nil || writes != 0 {
				t.Fatalf("error = %v, writes = %d", err, writes)
			}
		})
	}
}

func TestJSMReadPermissionErrorIsTyped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/servicedeskapi/servicedesk" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorMessages":["agent permission required"]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	_, err := client.JSMRequest(context.Background(), "HELP-1", "")
	var permission *PermissionError
	var jiraErr *Error
	if !errors.As(err, &permission) || !errors.As(err, &jiraErr) ||
		jiraErr.StatusCode != http.StatusForbidden {
		t.Fatalf("error = %#v", err)
	}
}

func TestPlatformCommandsDoNotRequireServiceManagement(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/rest/api/2/myself" {
			t.Fatalf("unexpected optional product request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"displayName":"Platform User"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	user, err := client.Myself(context.Background())
	if err != nil || user.DisplayName != "Platform User" || requests != 1 {
		t.Fatalf("user = %#v, error = %v, requests = %d", user, err, requests)
	}
}

func TestJSMValidationFailsBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)

	calls := []func() error{
		func() error {
			_, err := client.JSMServiceDesks(context.Background(), -1, 50)
			return err
		},
		func() error {
			_, err := client.JSMQueues(context.Background(), "bad", 0, 50, false)
			return err
		},
		func() error {
			_, err := client.JSMRequests(context.Background(), JSMRequestOptions{Start: 0, Limit: 101})
			return err
		},
		func() error {
			_, err := client.JSMRequest(context.Background(), " ", "")
			return err
		},
	}
	for _, call := range calls {
		if err := call(); err == nil {
			t.Fatal("validation returned nil")
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestCreateJSMRequestWriteIsNotRetried(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/servicedeskapi/servicedesk":
			_, _ = w.Write([]byte(`{"values":[]}`))
		case strings.HasSuffix(r.URL.Path, "/field"):
			_, _ = w.Write([]byte(`{"requestTypeFields":[{"fieldId":"summary","name":"Summary","required":true}]}`))
		default:
			writes++
			_, _ = io.Copy(io.Discard, r.Body)
			http.Error(w, "temporary", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 3})

	_, err := client.CreateJSMRequest(context.Background(), "7", "9", map[string]any{"summary": "Help"})
	if err == nil || writes != 1 {
		t.Fatalf("error = %v, writes = %d", err, writes)
	}
}
