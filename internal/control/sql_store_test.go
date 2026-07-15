package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLitePersistsLoginAndControlPlaneState(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "wiremesh.db")) + "?_pragma=foreign_keys(1)"
	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "integration-master-key", Store: store})
	if err != nil {
		t.Fatal(err)
	}

	loginBody := `{"email":"admin@wiremesh.local","password":"wiremesh-dev"}`
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginBody)))
	if response.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", response.Code, response.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil || session.Token == "" {
		t.Fatalf("missing login token: %v", err)
	}

	now := time.Now().UTC()
	project := Project{ID: "project_sql", TenantID: "tenant_demo", Name: "Persistent Project", Description: "SQLite", CreatedAt: now}
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	network := Network{ID: "network_sql", TenantID: "tenant_demo", ProjectID: project.ID, Name: "Persistent Network", CIDR: "10.90.0.0/24", DNS: "1.1.1.1", Topology: TopologyCustom, CreatedAt: now}
	if err := store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode("tenant_demo", network, "persistent-node", "edge.example:51820", "test", "linux", "0.1.0", map[string]string{"wiremesh.role": "hub"})
	if err != nil {
		t.Fatal(err)
	}
	peer := PeerRelation{ID: "peer_sql", TenantID: "tenant_demo", NetworkID: network.ID, SourceNodeID: node.ID, TargetNodeID: "another_node", CreatedAt: now}
	if err := store.AddPeer(peer); err != nil {
		t.Fatal(err)
	}
	revision := ConfigRevision{ID: "revision_sql", TenantID: "tenant_demo", ProjectID: project.ID, NetworkID: network.ID, Version: 1, Configs: map[string]NodeConfig{node.ID: {NodeID: node.ID, NetworkID: network.ID, Address: node.Address, PrivateKey: "private", ListenPort: 51820}}, CreatedAt: now}
	if err := store.CreateRevision(revision); err != nil {
		t.Fatal(err)
	}
	delivery := ConfigDelivery{ID: "delivery_sql", TenantID: "tenant_demo", NodeID: node.ID, Version: 1, State: "pending", UpdatedAt: now}
	if err := store.CreateDelivery(delivery); err != nil {
		t.Fatal(err)
	}
	delivery.State = "applied"
	if err := store.UpdateDelivery(delivery); err != nil {
		t.Fatal(err)
	}
	enrollment := EnrollmentToken{ID: "enrollment_sql", TenantID: "tenant_demo", ProjectID: project.ID, NetworkID: network.ID, Token: "one-time-token", ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollment(enrollment); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(enrollment.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(enrollment.Token); err == nil {
		t.Fatal("enrollment token was consumed twice")
	}
	identity := AgentIdentity{NodeID: node.ID, CertificatePEM: "certificate", CertificateFingerprint: "fingerprint", ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateIdentity(identity); err != nil {
		t.Fatal(err)
	}
	if err := store.AddAudit(AuditEvent{ID: "audit_sql", TenantID: "tenant_demo", ActorID: "usr_admin", Action: "test.persist", ResourceType: "node", ResourceID: node.ID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, err = NewApp(Config{MasterKey: "integration-master-key", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.auth.Login("admin@wiremesh.local", "wiremesh-dev"); err != nil {
		t.Fatalf("login did not survive restart: %v", err)
	}
	if projects := store.ListProjects("tenant_demo"); len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("projects not persisted: %#v", projects)
	}
	persistedNode, err := store.GetNode("tenant_demo", node.ID)
	if err != nil || persistedNode.Labels["wiremesh.role"] != "hub" || persistedNode.PrivateKey.Ciphertext == "" {
		t.Fatalf("node not persisted: %#v %v", persistedNode, err)
	}
	latest, err := store.LatestRevision("tenant_demo", network.ID)
	if err != nil || latest.Version != 1 || latest.Configs[node.ID].ListenPort != 51820 {
		t.Fatalf("revision not persisted: %#v %v", latest, err)
	}
	if deliveries := store.ListDeliveries("tenant_demo", node.ID); len(deliveries) != 1 || deliveries[0].State != "applied" {
		t.Fatalf("delivery not persisted: %#v", deliveries)
	}
	if _, err := store.GetIdentity(node.ID); err != nil {
		t.Fatalf("identity not persisted: %v", err)
	}
	if events := store.ListAudit("tenant_demo"); len(events) < 2 {
		t.Fatalf("audit events not persisted: %#v", events)
	}
}

func TestPostgresPlaceholderBinding(t *testing.T) {
	store := &SQLStore{driver: "postgres"}
	got := store.query("SELECT * FROM users WHERE tenant_id = ? AND email = ?")
	if got != "SELECT * FROM users WHERE tenant_id = $1 AND email = $2" {
		t.Fatalf("unexpected query: %s", got)
	}
}
