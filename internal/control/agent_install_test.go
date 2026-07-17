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

	body := strings.NewReader(`{"hostname":"edge-01","os":"linux/amd64","agent_version":"0.3.0","labels":{"env":"prod"},"interfaces":"auto","collection_error":"ip metadata unavailable","wireguard":[{"name":"wg0","public_key":"public-key","listen_port":51820,"addresses":["10.91.0.2/32"],"mtu":1420,"up":true,"peers":[{"public_key":"peer-key","endpoint":"198.51.100.10:51820","allowed_ips":["10.91.0.3/32"],"latest_handshake_at":"2026-07-17T08:00:00Z","receive_bytes":123,"transmit_bytes":456}]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", body)
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

	request = httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(`{}`))
	request.Header.Set("X-Agent-ID", "unknown-node")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown agent heartbeat returned %d", response.Code)
	}
}
