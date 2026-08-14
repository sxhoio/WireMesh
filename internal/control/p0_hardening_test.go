package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewAppRequiresMasterKey(t *testing.T) {
	if _, err := NewApp(Config{}); err == nil || !strings.Contains(err.Error(), "master key") {
		t.Fatalf("NewApp must reject empty master key, got %v", err)
	}
	if _, err := NewSecretBox(""); err == nil {
		t.Fatal("NewSecretBox must reject empty master key")
	}
}

func TestAgentCAPersistsAcrossRestart(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "wiremesh-ca.json")
	app1, err := NewApp(Config{MasterKey: "ca-persist-key", AgentInsecureHTTP: true, CAFile: caPath})
	if err != nil {
		t.Fatal(err)
	}
	app2, err := NewApp(Config{MasterKey: "ca-persist-key", AgentInsecureHTTP: true, CAFile: caPath})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(app1.ca.Raw, app2.ca.Raw) {
		t.Fatal("CA must be reused across restarts when persisted")
	}
	// 损坏文件必须报错而非静默重建
	if err := os.WriteFile(caPath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApp(Config{MasterKey: "ca-persist-key", AgentInsecureHTTP: true, CAFile: caPath}); err == nil {
		t.Fatal("corrupt CA file must fail startup")
	}
}

func TestAgentCARequiresSameMasterKey(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "wiremesh-ca.json")
	if _, err := NewApp(Config{MasterKey: "ca-key-a", AgentInsecureHTTP: true, CAFile: caPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApp(Config{MasterKey: "ca-key-b", AgentInsecureHTTP: true, CAFile: caPath}); err == nil {
		t.Fatal("CA encrypted under key A must not be readable with key B")
	}
}

func TestRevisionPrivateKeysSealedAtRest(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "seal-admin@example.com", "strong-password")
	project := Project{ID: "seal-p", TenantID: admin.TenantID, Name: "Seal", CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	network := Network{ID: "seal-n", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Seal", CIDR: "10.64.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "seal-node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.publishNetwork(admin.TenantID, network); err != nil {
		t.Fatal(err)
	}
	revision, err := app.store.LatestRevision(admin.TenantID, network.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored := revision.Configs[node.ID]
	if !strings.HasPrefix(stored.PrivateKey, "{") {
		t.Fatalf("private key must be encrypted at rest, got %q", stored.PrivateKey)
	}
	opened, err := app.openRevisionConfig(stored)
	if err != nil {
		t.Fatal(err)
	}
	if opened.PrivateKey == "" || strings.HasPrefix(opened.PrivateKey, "{") {
		t.Fatalf("openRevisionConfig must return plaintext key, got %q", opened.PrivateKey)
	}
	// 历史明文格式兼容
	legacy := NodeConfig{NodeID: node.ID, PrivateKey: "legacy-plaintext-key"}
	plain, err := app.openRevisionConfig(legacy)
	if err != nil || plain.PrivateKey != "legacy-plaintext-key" {
		t.Fatalf("legacy plaintext keys must pass through: %#v %v", plain, err)
	}
	// 再次发布无变更 → Unchanged（密封后的随机加密不能破坏变更检测）
	result, err := app.publishNetwork(admin.TenantID, network)
	if err != nil || !result.Unchanged {
		t.Fatalf("unchanged publish must report unchanged: %#v %v", result, err)
	}
}

func TestSessionTimeoutFromSettings(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "ttl-admin@example.com", "strong-password")
	settings := defaultSystemSettings(admin.TenantID)
	settings.SessionTimeoutMin = 5
	settings.UpdatedAt = time.Now()
	if err := app.store.UpsertSettings(settings); err != nil {
		t.Fatal(err)
	}
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/login", "", `{"email":"ttl-admin@example.com","password":"strong-password"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("login: %d %s", response.Code, response.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("unexpected login response: %s", response.Body.String())
	}
	claims, err := app.auth.Parse(session.Token)
	if err != nil {
		t.Fatal(err)
	}
	remaining := time.Unix(claims.Exp, 0).Sub(time.Now())
	if remaining < 4*time.Minute || remaining > 6*time.Minute {
		t.Fatalf("token expiry must follow SessionTimeoutMin (5m), got %v", remaining)
	}
}

func TestStrictAgentCertRequired(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "strict-cert-key", AgentInsecureHTTP: true, RequireAgentClientCert: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "certificate required") {
		t.Fatalf("strict mode must require client certificate: %d %s", response.Code, response.Body.String())
	}
}
