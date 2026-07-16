package control

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

	admin, token := initializeTestAdmin(t, app, "sqlite-admin@example.com", "strong-password")
	if token == "" {
		t.Fatal("missing login token")
	}

	now := time.Now().UTC()
	project := Project{ID: "project_sql", TenantID: admin.TenantID, Name: "Persistent Project", Description: "SQLite", CreatedAt: now}
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	network := Network{ID: "network_sql", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Persistent Network", CIDR: "10.90.0.0/24", DNS: "1.1.1.1", Topology: TopologyCustom, CreatedAt: now}
	if err := store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "persistent-node", "edge.example:51820", "test", "linux", "0.1.0", map[string]string{"wiremesh.role": "hub"})
	if err != nil {
		t.Fatal(err)
	}
	peer := PeerRelation{ID: "peer_sql", TenantID: admin.TenantID, NetworkID: network.ID, SourceNodeID: node.ID, TargetNodeID: "another_node", CreatedAt: now}
	if err := store.AddPeer(peer); err != nil {
		t.Fatal(err)
	}
	revision := ConfigRevision{ID: "revision_sql", TenantID: admin.TenantID, ProjectID: project.ID, NetworkID: network.ID, Version: 1, Configs: map[string]NodeConfig{node.ID: {NodeID: node.ID, NetworkID: network.ID, Address: node.Address, PrivateKey: "private", ListenPort: 51820}}, CreatedAt: now}
	if err := store.CreateRevision(revision); err != nil {
		t.Fatal(err)
	}
	delivery := ConfigDelivery{ID: "delivery_sql", TenantID: admin.TenantID, NodeID: node.ID, Version: 1, State: "pending", UpdatedAt: now}
	if err := store.CreateDelivery(delivery); err != nil {
		t.Fatal(err)
	}
	delivery.State = "applied"
	if err := store.UpdateDelivery(delivery); err != nil {
		t.Fatal(err)
	}
	enrollment := EnrollmentToken{ID: "enrollment_sql", TenantID: admin.TenantID, ProjectID: project.ID, NetworkID: network.ID, Token: "one-time-token", ExpiresAt: now.Add(time.Hour)}
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
	if err := store.AddAudit(AuditEvent{ID: "audit_sql", TenantID: admin.TenantID, ActorID: admin.ID, Action: "test.persist", ResourceType: "node", ResourceID: node.ID, CreatedAt: now}); err != nil {
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
	if _, _, err := app.auth.Login("sqlite-admin@example.com", "strong-password"); err != nil {
		t.Fatalf("login did not survive restart: %v", err)
	}
	if projects := store.ListProjects(admin.TenantID); len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("projects not persisted: %#v", projects)
	}
	persistedNode, err := store.GetNode(admin.TenantID, node.ID)
	if err != nil || persistedNode.Labels["wiremesh.role"] != "hub" || persistedNode.PrivateKey.Ciphertext == "" {
		t.Fatalf("node not persisted: %#v %v", persistedNode, err)
	}
	latest, err := store.LatestRevision(admin.TenantID, network.ID)
	if err != nil || latest.Version != 1 || latest.Configs[node.ID].ListenPort != 51820 {
		t.Fatalf("revision not persisted: %#v %v", latest, err)
	}
	if deliveries := store.ListDeliveries(admin.TenantID, node.ID); len(deliveries) != 1 || deliveries[0].State != "applied" {
		t.Fatalf("delivery not persisted: %#v", deliveries)
	}
	if _, err := store.GetIdentity(node.ID); err != nil {
		t.Fatalf("identity not persisted: %v", err)
	}
	if events := store.ListAudit(admin.TenantID); len(events) < 2 {
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

func TestSQLiteInitialSetupIsAtomic(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "wiremesh.db")) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, err := NewApp(Config{MasterKey: "concurrent-setup-key", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Router()
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, email := range []string{"first@example.com", "second@example.com"} {
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			<-start
			body := `{"email":"` + email + `","name":"Administrator","password":"strong-password"}`
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body)))
			codes <- response.Code
		}(email)
	}
	close(start)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("expected one successful and one rejected setup, got %#v", counts)
	}
}
