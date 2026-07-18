package control

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentInstallerAndBinaryDownload(t *testing.T) {
	binaryTemplate := filepath.Join(t.TempDir(), "wiremesh-agent-{os}-{arch}")
	binaryPath := strings.ReplaceAll(strings.ReplaceAll(binaryTemplate, "{os}", "linux"), "{arch}", "amd64")
	binary := []byte("test-agent-binary")
	if err := os.WriteFile(binaryPath, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "test-key", AgentBinaryPath: binaryTemplate})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Router()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/agent/install.sh", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("installer returned %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/x-shellscript") {
		t.Fatalf("unexpected installer content type %q", contentType)
	}
	for _, required := range []string{"--server", "--token", "wiremesh-agent.service", "/agent/download", "install_wireguard", "wireguard-tools", "/etc/wireguard"} {
		if !strings.Contains(response.Body.String(), required) {
			t.Fatalf("installer is missing %q", required)
		}
	}

	response = httptest.NewRecorder()
	path := "/agent/download?os=linux&arch=amd64"
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("agent download returned %d: %s", response.Code, response.Body.String())
	}
	downloaded, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(downloaded) != string(binary) {
		t.Fatalf("unexpected downloaded binary %q", downloaded)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/agent/download?os=unsupported&arch=unsupported", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unsupported platform returned %d", response.Code)
	}
}

func TestAgentUninstallerScriptIsDirectlyExecutable(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/agent/uninstall.sh", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("uninstaller returned %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/x-shellscript") {
		t.Fatalf("unexpected uninstaller content type %q", contentType)
	}
	script := response.Body.String()
	for _, required := range []string{"#!/usr/bin/env bash", "systemctl disable --now wiremesh-agent.service", "rm -f /usr/local/bin/wiremesh-agent", "rm -rf /var/lib/wiremesh-agent /etc/wiremesh-agent"} {
		if !strings.Contains(script, required) {
			t.Fatalf("uninstaller is missing %q", required)
		}
	}
}

func TestAgentInstallerUsesRequestURLAndBuiltInDefaults(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://wiremesh.internal/agent/install.sh", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "wiremesh.example.com")
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("installer returned %d: %s", response.Code, response.Body.String())
	}
	script := response.Body.String()
	for _, required := range []string{
		"SERVER='https://wiremesh.example.com'",
		`INTERFACES="auto"`,
		`REPORT_INTERVAL="10s"`,
		`PROBE_INTERVAL="15s"`,
		`https://*) USE_MTLS="true"`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer is missing default %q", required)
		}
	}
	if strings.Contains(script, "__WIREMESH_SERVER__") {
		t.Fatal("installer still contains the server placeholder")
	}
}

func TestAgentInstallerRecognizesHTTPSProxyHeaders(t *testing.T) {
	tests := []struct {
		name   string
		header string
		value  string
	}{
		{name: "standard Forwarded", header: "Forwarded", value: "for=192.0.2.1;proto=https;host=wiremesh.example.com"},
		{name: "forwarded SSL", header: "X-Forwarded-Ssl", value: "on"},
		{name: "frontend HTTPS", header: "Front-End-Https", value: "on"},
		{name: "forwarded port", header: "X-Forwarded-Port", value: "443"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://wiremesh.example.com/agent/install.sh", nil)
			request.Header.Set(test.header, test.value)
			if serverURL := agentInstallerServerURL(request); serverURL != "https://wiremesh.example.com" {
				t.Fatalf("server URL = %q, want HTTPS URL", serverURL)
			}
		})
	}
}

func TestAgentHeartbeatUpdatesRealNode(t *testing.T) {
	app := testApp(t)
	network := Network{
		ID: "heartbeat-network", TenantID: "heartbeat-tenant", ProjectID: "heartbeat-project",
		Name: "Heartbeat", CIDR: "10.91.0.0/24", Topology: TopologyFullMesh,
	}
	if err := app.store.CreateProject(Project{ID: network.ProjectID, TenantID: network.TenantID, Name: "Heartbeat", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(network.TenantID, network, "heartbeat-node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	payload := `{"hostname":"edge-01","os":"linux/amd64","agent_version":"0.3.0","labels":{"env":"prod"},"interfaces":"auto","collection_error":"ip metadata unavailable","wireguard":[{"name":"wg0","public_key":"public-key","listen_port":51111,"addresses":["10.91.0.2/32"],"mtu":1380,"up":true,"peers":[{"public_key":"peer-key","endpoint":"198.51.100.10:51820","allowed_ips":["10.91.0.3/32"],"latest_handshake_at":"2026-07-17T08:00:00Z","receive_bytes":123,"transmit_bytes":456}]}]}`
	request := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(payload))
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat returned %d: %s", response.Code, response.Body.String())
	}
	updated, err := app.store.GetNodeByID(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastSeen.IsZero() || updated.Hostname != "edge-01" || updated.InterfaceSelector != "auto" || updated.CollectionError != "ip metadata unavailable" || updated.OS != "linux/amd64" || updated.AgentVersion != "0.3.0" || updated.Labels["env"] != "prod" || len(updated.WireGuard) != 1 || updated.WireGuard[0].Peers[0].ReceiveBytes != 123 {
		t.Fatalf("heartbeat was not persisted: %#v", updated)
	}
	if updated.Address != "10.91.0.2" || updated.ListenPort != 51111 || updated.MTU != 1380 {
		t.Fatalf("reported WireGuard configuration was not adopted: %#v", updated)
	}
	foundAdoptionAudit := false
	for _, event := range app.store.ListAudit(network.TenantID) {
		if event.ResourceID == node.ID && event.Action == "agent.config.observed" {
			foundAdoptionAudit = true
			break
		}
	}
	if !foundAdoptionAudit {
		t.Fatal("reported WireGuard configuration adoption was not audited")
	}

	updated.Address = "10.91.0.3"
	updated.ListenPort = 52222
	updated.MTU = 1400
	if err := app.store.UpdateNode(updated); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(payload))
	request.Header.Set("X-Agent-ID", node.ID)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("second heartbeat returned %d: %s", response.Code, response.Body.String())
	}
	preserved, err := app.store.GetNodeByID(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Address != "10.91.0.3" || preserved.ListenPort != 52222 || preserved.MTU != 1400 {
		t.Fatalf("saved node configuration was overwritten by a later heartbeat: %#v", preserved)
	}

	request = httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(`{}`))
	request.Header.Set("X-Agent-ID", "unknown-node")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown agent heartbeat returned %d", response.Code)
	}
}
