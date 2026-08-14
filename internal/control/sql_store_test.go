package control

import (
	"database/sql"
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
	app, err := NewApp(Config{MasterKey: "integration-master-key", AgentInsecureHTTP: true, Store: store})
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
	settings := defaultSystemSettings(admin.TenantID)
	settings.DashboardName = "Persistent Settings"
	settings.UpdatedAt = now
	if err := store.UpsertSettings(settings); err != nil {
		t.Fatal(err)
	}
	target, err := app.box.Encrypt([]byte("https://hooks.example.com/test"))
	if err != nil {
		t.Fatal(err)
	}
	channel := NotificationChannel{ID: "channel_sql", TenantID: admin.TenantID, Name: "Persistent Webhook", Type: "webhook", Target: target, Enabled: true, AllAgents: true, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateNotificationChannel(channel); err != nil {
		t.Fatal(err)
	}
	notificationLog := NotificationLog{ID: "notification_log_sql", TenantID: admin.TenantID, ChannelID: channel.ID, ChannelName: channel.Name, ChannelType: channel.Type, AgentName: node.Name, Message: "persistent notification", Status: "success", CreatedAt: now}
	if err := store.AddNotificationLog(notificationLog); err != nil {
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
	app, err = NewApp(Config{MasterKey: "integration-master-key", AgentInsecureHTTP: true, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	_, loggedInUser, err := app.auth.Login("sqlite-admin@example.com", "strong-password")
	if err != nil {
		t.Fatalf("login did not survive restart: %v", err)
	}
	if loggedInUser.LastLoginAt.IsZero() {
		t.Fatal("login time was not returned after restart")
	}
	persistedUser, err := store.GetUser(admin.ID)
	if err != nil || !persistedUser.LastLoginAt.Equal(loggedInUser.LastLoginAt) {
		t.Fatalf("login time was not persisted after restart: %#v %v", persistedUser, err)
	}
	if projects, err := store.ListProjects(admin.TenantID); err != nil || len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("projects not persisted: %#v %v", projects, err)
	}
	persistedNode, err := store.GetNode(admin.TenantID, node.ID)
	if err != nil || persistedNode.Labels["wiremesh.role"] != "hub" || persistedNode.PrivateKey.Ciphertext == "" {
		t.Fatalf("node not persisted: %#v %v", persistedNode, err)
	}
	latest, err := store.LatestRevision(admin.TenantID, network.ID)
	if err != nil || latest.Version != 1 || latest.Configs[node.ID].ListenPort != 51820 {
		t.Fatalf("revision not persisted: %#v %v", latest, err)
	}
	if deliveries, err := store.ListDeliveries(admin.TenantID, node.ID); err != nil || len(deliveries) != 1 || deliveries[0].State != "applied" {
		t.Fatalf("delivery not persisted: %#v %v", deliveries, err)
	}
	if _, err := store.GetIdentity(node.ID); err != nil {
		t.Fatalf("identity not persisted: %v", err)
	}
	if events, err := store.ListAudit(admin.TenantID); err != nil || len(events) < 2 {
		t.Fatalf("audit events not persisted: %#v %v", events, err)
	}
	persistedSettings, err := store.GetSettings(admin.TenantID)
	if err != nil || persistedSettings.DashboardName != settings.DashboardName {
		t.Fatalf("settings not persisted: %#v %v", persistedSettings, err)
	}
	channels, err := store.ListNotificationChannels(admin.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].ID != channel.ID {
		t.Fatalf("notification channel not persisted: %#v", channels)
	}
	plaintext, err := app.box.Decrypt(channels[0].Target)
	if err != nil || string(plaintext) != "https://hooks.example.com/test" {
		t.Fatalf("notification target not persisted or decryptable: %q %v", plaintext, err)
	}
	logs, err := store.ListNotificationLogs(admin.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != notificationLog.ID || logs[0].Message != notificationLog.Message {
		t.Fatalf("notification log not persisted: %#v", logs)
	}
}

func TestSQLiteMigratesExistingUsersLastLoginColumn(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "legacy-wiremesh.db"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	passwordHash, err := hashPassword("strong-password")
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().UTC().Add(-time.Hour)
	if _, err := db.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "legacy-user", "legacy-tenant", "legacy@example.com", passwordHash, "Legacy User", string(RoleAdmin), timeText(createdAt)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'last_login_at'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("last_login_at column was not migrated: count=%d err=%v", count, err)
	}
	loginAt := time.Now().UTC()
	if err := store.UpdateUserLastLogin("legacy-user", loginAt); err != nil {
		t.Fatalf("update migrated login time: %v", err)
	}
	user, err := store.GetUser("legacy-user")
	if err != nil || !user.LastLoginAt.Equal(loginAt) {
		t.Fatalf("read migrated login time: %#v %v", user, err)
	}
}

func TestSQLiteMigratesExistingSystemSettingsGeoIPColumn(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "legacy-settings.db"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE system_settings (tenant_id TEXT PRIMARY KEY, settings_json TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Now().UTC().Add(-time.Hour)
	if _, err := db.Exec(`INSERT INTO system_settings (tenant_id, settings_json, updated_at) VALUES (?, ?, ?)`, "legacy-tenant", `{"dashboardName":"Legacy"}`, timeText(updatedAt)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatalf("open legacy settings database: %v", err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('system_settings') WHERE name = 'geoip_db_path'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("geoip_db_path column was not migrated: count=%d err=%v", count, err)
	}
	settings, err := store.GetSettings("legacy-tenant")
	if err != nil {
		t.Fatalf("read migrated settings: %v", err)
	}
	if settings.GeoIPDBPath != "" || settings.DashboardName != "Legacy" {
		t.Fatalf("unexpected migrated settings: %#v", settings)
	}
}

func TestSQLiteMigratesNodeWireGuardColumn(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "legacy-nodes.db"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE nodes (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, network_id TEXT NOT NULL, name TEXT NOT NULL, address TEXT NOT NULL, endpoint TEXT NOT NULL, region TEXT NOT NULL, os TEXT NOT NULL, agent_version TEXT NOT NULL, labels_json TEXT NOT NULL, public_key TEXT NOT NULL, private_key_json TEXT NOT NULL, last_seen TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO nodes (id, tenant_id, project_id, network_id, name, address, endpoint, region, os, agent_version, labels_json, public_key, private_key_json, last_seen, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-node", "legacy-tenant", "legacy-project", "legacy-network", "Legacy", "10.0.0.1/32", "", "", "linux", "0.2.0", "{}", "public", "{}", timeText(now), timeText(now)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatalf("open legacy node database: %v", err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name = 'wireguard_json'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("wireguard_json column was not migrated: count=%d err=%v", count, err)
	}
	node, err := store.GetNode("legacy-tenant", "legacy-node")
	if err != nil || node.WireGuard == nil || len(node.WireGuard) != 0 {
		t.Fatalf("legacy WireGuard status was not initialized: %#v %v", node, err)
	}
	node.WireGuard = []WireGuardInterfaceStatus{{Name: "wg0", ListenPort: 51820, Up: true}}
	if err := store.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	node, err = store.GetNode("legacy-tenant", "legacy-node")
	if err != nil || len(node.WireGuard) != 1 || node.WireGuard[0].Name != "wg0" {
		t.Fatalf("WireGuard status was not persisted: %#v %v", node, err)
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
	app, err := NewApp(Config{MasterKey: "concurrent-setup-key", AgentInsecureHTTP: true, Store: store})
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

func TestSQLiteNodeCommandsAndDeleteCascade(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "wiremesh-node-commands.db")) + "?_pragma=foreign_keys(1)"
	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	project := Project{ID: "project_node_commands", TenantID: "tenant_node_commands", Name: "Node Commands", CreatedAt: now}
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	network := Network{ID: "network_node_commands", TenantID: project.TenantID, ProjectID: project.ID, Name: "Node Commands", CIDR: "10.88.0.0/24", Topology: TopologyCustom, CreatedAt: now}
	if err := store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node := Node{ID: "node_commands", TenantID: project.TenantID, ProjectID: project.ID, NetworkID: network.ID, Name: "Managed Node", Enabled: true, ListenPort: 51821, MTU: 1380, Address: "10.88.0.10", Endpoint: "node.example:51821", Labels: map[string]string{"wiremesh.role": "mesh"}, CreatedAt: now}
	other := Node{ID: "node_commands_other", TenantID: project.TenantID, ProjectID: project.ID, NetworkID: network.ID, Name: "Other Node", Enabled: true, Address: "10.88.0.11", Labels: map[string]string{}, CreatedAt: now}
	if err := store.CreateNode(node); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(other); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.GetNode(project.TenantID, node.ID)
	if err != nil || !persisted.Enabled || persisted.ListenPort != 51821 || persisted.MTU != 1380 {
		t.Fatalf("custom node settings were not persisted: %#v %v", persisted, err)
	}
	if err := store.AddPeer(PeerRelation{ID: "peer_node_commands", TenantID: project.TenantID, NetworkID: network.ID, SourceNodeID: node.ID, TargetNodeID: other.ID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDelivery(ConfigDelivery{ID: "delivery_node_commands", TenantID: project.TenantID, NodeID: node.ID, Version: 1, State: "pending", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateIdentity(AgentIdentity{NodeID: node.ID, CertificatePEM: "certificate", CertificateFingerprint: "fingerprint", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	command := AgentCommand{ID: "command_node_commands", TenantID: project.TenantID, NodeID: node.ID, Type: "collect", State: "pending", CreatedAt: now}
	if err := store.CreateCommand(command); err != nil {
		t.Fatal(err)
	}
	claimed := store.ClaimCommands(node.ID)
	if len(claimed) != 1 || claimed[0].ID != command.ID || claimed[0].State != "running" || claimed[0].StartedAt == nil {
		t.Fatalf("command was not claimed: %#v", claimed)
	}
	if duplicate := store.ClaimCommands(node.ID); len(duplicate) != 0 {
		t.Fatalf("command was claimed twice: %#v", duplicate)
	}
	completedAt := now.Add(time.Second)
	claimed[0].State = "completed"
	claimed[0].Result = "wireguard_interfaces=1"
	claimed[0].CompletedAt = &completedAt
	if err := store.UpdateCommand(claimed[0]); err != nil {
		t.Fatal(err)
	}
	commands, err := store.ListCommands(project.TenantID, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].State != "completed" || commands[0].Result != "wireguard_interfaces=1" || commands[0].CompletedAt == nil {
		t.Fatalf("command result was not persisted: %#v", commands)
	}

	if err := store.DeleteNode(project.TenantID, node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNode(project.TenantID, node.ID); err == nil {
		t.Fatal("deleted node is still present")
	}
	if _, err := store.GetIdentity(node.ID); err == nil {
		t.Fatal("deleted node identity is still present")
	}
	if peers, err := store.ListPeers(project.TenantID, network.ID); err != nil || len(peers) != 0 {
		t.Fatalf("deleted node peers are still present: %#v %v", peers, err)
	}
	if deliveries, err := store.ListDeliveries(project.TenantID, node.ID); err != nil || len(deliveries) != 0 {
		t.Fatalf("deleted node deliveries are still present: %#v %v", deliveries, err)
	}
	if commands, err := store.ListCommands(project.TenantID, node.ID); err != nil || len(commands) != 0 {
		t.Fatalf("deleted node commands are still present: %#v %v", commands, err)
	}
	if _, err := store.GetNode(project.TenantID, other.ID); err != nil {
		t.Fatalf("unrelated node was deleted: %v", err)
	}
}

func TestSQLiteTrafficSamples(t *testing.T) {
	store, err := OpenSQLStore("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "traffic.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Second)
	node := Node{ID: "node-traffic", TenantID: "tenant-traffic", ProjectID: "project-traffic", NetworkID: "network-traffic", Name: "Traffic Node", Address: "10.0.0.2", Labels: map[string]string{}, WireGuard: []WireGuardInterfaceStatus{}, CreatedAt: now, LastSeen: now}
	if err := store.CreateNode(node); err != nil {
		t.Fatal(err)
	}
	samples := []TrafficSample{
		{ID: "traffic-1", TenantID: node.TenantID, NodeID: node.ID, InterfaceName: "wg0", ReceiveBytes: 1000, TransmitBytes: 2000, RecordedAt: now},
		{ID: "traffic-2", TenantID: node.TenantID, NodeID: node.ID, InterfaceName: "wg0", ReceiveBytes: 1001000, TransmitBytes: 502000, RecordedAt: now.Add(10 * time.Second)},
	}
	if err := store.AddTrafficSamples(samples); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListTrafficSamples(node.TenantID, node.ID, "wg0", now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ReceiveBytes != samples[1].ReceiveBytes {
		t.Fatalf("unexpected traffic samples: %#v", got)
	}
	if err := store.DeleteNode(node.TenantID, node.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ListTrafficSamples(node.TenantID, node.ID, "wg0", now.Add(-time.Second)); err != nil || len(got) != 0 {
		t.Fatalf("traffic samples not deleted: %#v %v", got, err)
	}
}
