package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveControlPlaneURLFollowsHTTPSRedirect(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/healthz" {
			t.Fatalf("unexpected health request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer tlsServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tlsServer.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer redirectServer.Close()

	resolved, err := resolveControlPlaneURL(redirectServer.URL, tlsServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	if resolved != tlsServer.URL {
		t.Fatalf("resolved URL = %q, want %q", resolved, tlsServer.URL)
	}
}

func TestResolveControlPlaneURLRejectsUnexpectedHealthResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"starting"}`))
	}))
	defer server.Close()

	if _, err := resolveControlPlaneURL(server.URL, server.Client()); err == nil {
		t.Fatal("expected an invalid health response to be rejected")
	}
}

func TestSetDevelopmentIdentitySupportsHTTPSReverseProxy(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://wiremesh.example.com/agent/v1/heartbeat", nil)
	setDevelopmentIdentity(request, agentState{NodeID: "node-test", Server: "https://wiremesh.example.com"})
	if value := request.Header.Get("X-Agent-ID"); value != "node-test" {
		t.Fatalf("X-Agent-ID = %q, want node-test", value)
	}
}

func TestSetDevelopmentIdentityAttachesPublicIP(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://wiremesh.example.com/agent/v1/heartbeat", nil)
	setDevelopmentIdentity(request, agentState{NodeID: "node-test", Server: "http://wiremesh.example.com", PublicIP: "203.0.113.9"})
	if value := request.Header.Get("X-Agent-Public-IP"); value != "203.0.113.9" {
		t.Fatalf("X-Agent-Public-IP = %q, want 203.0.113.9", value)
	}

	noIP := httptest.NewRequest(http.MethodPost, "http://wiremesh.example.com/agent/v1/heartbeat", nil)
	setDevelopmentIdentity(noIP, agentState{NodeID: "node-test", Server: "http://wiremesh.example.com"})
	if value := noIP.Header.Get("X-Agent-Public-IP"); value != "" {
		t.Fatalf("X-Agent-Public-IP must be omitted when unknown, got %q", value)
	}
}

func TestAuthenticatedClientAllowsPreEnrollmentHTTPSProbe(t *testing.T) {
	client, err := authenticatedClient(agentState{Server: "http://wiremesh.example.com"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if client.Transport == nil {
		t.Fatal("expected a configured TLS transport")
	}
}

func TestPollAgentCommandsUsesLongPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agent/v1/commands" {
			t.Fatalf("unexpected command request: %s %s", r.Method, r.URL.Path)
		}
		if value := r.URL.Query().Get("wait"); value != agentCommandWait.String() {
			t.Fatalf("wait = %q, want %q", value, agentCommandWait)
		}
		if value := r.Header.Get("X-Agent-ID"); value != "node-test" {
			t.Fatalf("X-Agent-ID = %q", value)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-WireMesh-Command-Long-Poll", "true")
		_, _ = w.Write([]byte(`[{"id":"cmd-test","type":"collect","state":"running"}]`))
	}))
	defer server.Close()

	commands, longPollSupported, err := pollAgentCommands(t.Context(), server.Client(), agentState{NodeID: "node-test", Server: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !longPollSupported {
		t.Fatal("server long-poll capability header was not recognized")
	}
	if len(commands) != 1 || commands[0].ID != "cmd-test" || commands[0].Type != "collect" {
		t.Fatalf("unexpected commands: %#v", commands)
	}
}
