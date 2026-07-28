package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerInfoDetectsDeployment(t *testing.T) {
	tests := []struct {
		name           string
		deploymentType string
		want           Deployment
	}{
		{name: "cloud", deploymentType: "Cloud", want: DeploymentCloud},
		{name: "data center", deploymentType: "Data Center", want: DeploymentDataCenter},
		{name: "server", deploymentType: "Server", want: DeploymentDataCenter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/agile/1.0/board" {
					if r.URL.Query().Get("maxResults") != "1" {
						t.Fatalf("query = %q", r.URL.RawQuery)
					}
					_, _ = w.Write([]byte(`{"values":[]}`))
					return
				}
				if r.URL.Path != "/rest/api/2/serverInfo" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"baseUrl":        "https://jira.example.com",
					"version":        "10.3.1",
					"deploymentType": tt.deploymentType,
				})
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "", DeploymentAuto, time.Second)
			info, err := client.ServerInfo(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if info.Deployment != tt.want {
				t.Fatalf("Deployment = %q, want %q", info.Deployment, tt.want)
			}
			if info.DeploymentSource != "detected" {
				t.Fatalf("DeploymentSource = %q", info.DeploymentSource)
			}
			if info.Capabilities.Platform != CapabilityAvailable {
				t.Fatalf("platform capability = %q", info.Capabilities.Platform)
			}
			if info.Capabilities.Software != CapabilityAvailable {
				t.Fatalf("software capability = %q", info.Capabilities.Software)
			}
		})
	}
}

func TestDeploymentOverrideSkipsDetection(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/rest/api/2/serverInfo" {
			t.Fatal("deployment override should skip detection")
		}
		if r.URL.Path != "/rest/api/2/myself" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"displayName":"Agent"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "stale@example.com", DeploymentDataCenter, time.Second)
	user, err := client.Myself(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Agent" {
		t.Fatalf("DisplayName = %q", user.DisplayName)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestServerInfoReportsConfiguredOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1001.0.0","deploymentType":"Cloud"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	info, err := client.ServerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Deployment != DeploymentDataCenter {
		t.Fatalf("Deployment = %q", info.Deployment)
	}
	if info.DeploymentSource != "config" {
		t.Fatalf("DeploymentSource = %q", info.DeploymentSource)
	}
}

func TestServerInfoReturnsPartialInfoWhenOverrideEndpointIsBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentDataCenter, time.Second)
	info, err := client.ServerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Deployment != DeploymentDataCenter || info.DeploymentSource != "config" {
		t.Fatalf("info = %#v", info)
	}
	if info.BaseURL != server.URL {
		t.Fatalf("BaseURL = %q", info.BaseURL)
	}
	if info.Version != "" {
		t.Fatalf("Version = %q, want empty", info.Version)
	}
}

func TestPlatformPathSelectsOperationVersion(t *testing.T) {
	tests := []struct {
		deployment Deployment
		want       string
	}{
		{deployment: DeploymentCloud, want: "/rest/api/3/search/jql"},
		{deployment: DeploymentDataCenter, want: "/rest/api/2/search/jql"},
	}
	for _, tt := range tests {
		client := NewClient("https://jira.example.com", "token", "", tt.deployment, time.Second)
		got, err := client.PlatformPath(context.Background(), 3, "/search/jql")
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("deployment %q path = %q, want %q", tt.deployment, got, tt.want)
		}
	}
}

func TestCloudBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "agent@example.com" || password != "cloud-token" {
			t.Fatalf("BasicAuth = %q %q %v", user, password, ok)
		}
		_, _ = w.Write([]byte(`{"displayName":"Agent"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "cloud-token", "agent@example.com", DeploymentCloud, time.Second)
	if _, err := client.Myself(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDetectedDeploymentPreservesIssueOperations(t *testing.T) {
	tests := []struct {
		name           string
		deploymentType string
		wantAuthPrefix string
		cloudFallback  bool
	}{
		{name: "cloud", deploymentType: "Cloud", wantAuthPrefix: "Basic ", cloudFallback: true},
		{name: "data center", deploymentType: "Data Center", wantAuthPrefix: "Bearer ", cloudFallback: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodies = map[string]map[string]any{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/api/2/serverInfo" {
					if tt.cloudFallback && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
						http.Error(w, "use Cloud Basic auth", http.StatusUnauthorized)
						return
					}
					if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, tt.wantAuthPrefix) {
						t.Fatalf("serverInfo Authorization = %q, want prefix %q", got, tt.wantAuthPrefix)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"deploymentType":"` + tt.deploymentType + `"}`))
					return
				}

				if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, tt.wantAuthPrefix) {
					t.Fatalf("%s %s Authorization = %q, want prefix %q", r.Method, r.URL.Path, got, tt.wantAuthPrefix)
				}
				if r.Body != nil && r.ContentLength != 0 {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					bodies[r.Method+" "+r.URL.Path] = body
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1":
					_, _ = w.Write([]byte(`{"id":"1","key":"ENG-1","fields":{"summary":"Test"}}`))
				case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue":
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id":"2","key":"ENG-2"}`))
				case r.Method == http.MethodPut && r.URL.Path == "/rest/api/2/issue/ENG-1":
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ENG-1/comment":
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"body":"hello"}`))
				case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ENG-1/transitions":
					_, _ = w.Write([]byte(`{"transitions":[{"id":"31","name":"Done"}]}`))
				case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ENG-1/transitions":
					w.WriteHeader(http.StatusNoContent)
				default:
					http.Error(w, "unexpected request", http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, "token", "agent@example.com", DeploymentAuto, time.Second)
			ctx := context.Background()
			if _, err := client.GetIssue(ctx, "ENG-1", "summary"); err != nil {
				t.Fatal(err)
			}
			if _, err := client.CreateIssue(ctx, "ENG", "Task", "Created", "Details"); err != nil {
				t.Fatal(err)
			}
			if err := client.UpdateIssue(ctx, "ENG-1", map[string]any{"summary": "Updated"}); err != nil {
				t.Fatal(err)
			}
			if _, err := client.AddComment(ctx, "ENG-1", "hello"); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Transitions(ctx, "ENG-1"); err != nil {
				t.Fatal(err)
			}
			if err := client.TransitionIssue(ctx, "ENG-1", "31"); err != nil {
				t.Fatal(err)
			}

			createFields := bodies["POST /rest/api/2/issue"]["fields"].(map[string]any)
			if createFields["summary"] != "Created" {
				t.Fatalf("create fields = %#v", createFields)
			}
			updateFields := bodies["PUT /rest/api/2/issue/ENG-1"]["fields"].(map[string]any)
			if updateFields["summary"] != "Updated" {
				t.Fatalf("update fields = %#v", updateFields)
			}
			if bodies["POST /rest/api/2/issue/ENG-1/comment"]["body"] != "hello" {
				t.Fatalf("comment body = %#v", bodies["POST /rest/api/2/issue/ENG-1/comment"])
			}
			transition := bodies["POST /rest/api/2/issue/ENG-1/transitions"]["transition"].(map[string]any)
			if transition["id"] != "31" {
				t.Fatalf("transition = %#v", transition)
			}
		})
	}
}

func TestAutoDetectionFailureSuggestsOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", "", DeploymentAuto, time.Second)
	_, err := client.Myself(context.Background())
	if err == nil {
		t.Fatal("expected detection failure")
	}
	if !strings.Contains(err.Error(), "set jira.deployment") {
		t.Fatalf("error = %q", err)
	}
}

func TestParseDeploymentRejectsUnknownValue(t *testing.T) {
	if _, err := ParseDeployment("hosted"); err == nil {
		t.Fatal("expected invalid deployment error")
	}
}
