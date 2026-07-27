package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	app, err := NewApp(Config{MasterKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func initializeTestAdmin(t *testing.T, app *App, email, password string) (User, string) {
	t.Helper()
	body := `{"email":"` + email + `","name":"Test Administrator","password":"` + password + `"}`
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("setup failed: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		User User `json:"user"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	loginBody := `{"email":"` + email + `","password":"` + password + `"}`
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginBody)))
	if response.Code != http.StatusOK {
		t.Fatalf("new administrator cannot log in: %d %s", response.Code, response.Body.String())
	}
	var session struct {
		Token string `json:"token"`
		User  User   `json:"user"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.Token == "" || session.User.ID != result.User.ID {
		t.Fatalf("login returned unexpected session: %#v", session)
	}
	return session.User, session.Token
}

func TestInitialSetupFlow(t *testing.T) {
	app := testApp(t)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"initialized":false`) {
		t.Fatalf("unexpected empty setup status: %d %s", response.Code, response.Body.String())
	}

	user, _ := initializeTestAdmin(t, app, "OWNER@EXAMPLE.COM", "strong-password")
	if user.Email != "owner@example.com" || user.Role != RoleAdmin || user.TenantID == "" || user.ID == "" {
		t.Fatalf("unexpected initial administrator: %#v", user)
	}

	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"initialized":true`) {
		t.Fatalf("unexpected initialized setup status: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{"email":"other@example.com","name":"Other","password":"another-password"}`)))
	if response.Code != http.StatusConflict {
		t.Fatalf("second setup should conflict: %d %s", response.Code, response.Body.String())
	}
}

func TestEmptyListEndpointsReturnArrays(t *testing.T) {
	app := testApp(t)
	_, token := initializeTestAdmin(t, app, "empty@example.com", "strong-password")
	for _, path := range []string{
		"/api/v1/projects",
		"/api/v1/networks?project_id=missing",
		"/api/v1/nodes",
		"/api/v1/deliveries",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		app.Router().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
		var items []json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil || items == nil {
			t.Fatalf("%s must return a JSON array, got %s (%v)", path, response.Body.String(), err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("/api/v1/audit returned %d: %s", response.Code, response.Body.String())
	}
	var page AuditLogPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil || page.Items == nil || page.Limit == 0 {
		t.Fatalf("/api/v1/audit must return a paged JSON response, got %s (%v)", response.Body.String(), err)
	}
}

func TestAuditLogsArePagedAndClearable(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "audit-page@example.com", "strong-password")
	for index := 0; index < 3; index++ {
		app.auditEvent(admin.TenantID, admin.ID, "test.audit", "node", "node-"+string(rune('a'+index)), nil)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit?limit=2", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("audit page failed: %d %s", response.Code, response.Body.String())
	}
	var page AuditLogPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || !page.HasMore || page.Limit != 2 || page.Offset != 0 {
		t.Fatalf("unexpected audit page: %#v", page)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/audit", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("clear audit failed: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("read cleared audit failed: %d %s", response.Code, response.Body.String())
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.HasMore {
		t.Fatalf("audit logs were not cleared: %#v", page)
	}
}

func TestEnrolledNodeAppearsInNodeListWithoutWireGuard(t *testing.T) {
	app := testApp(t)
	admin, sessionToken := initializeTestAdmin(t, app, "nodes@example.com", "strong-password")
	project := Project{ID: "project-nodes", TenantID: admin.TenantID, Name: "Nodes", CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	network := Network{ID: "network-nodes", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Nodes", CIDR: "10.44.0.0/24", Topology: TopologyCustom, CreatedAt: time.Now()}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateEnrollment(EnrollmentToken{ID: "enroll-nodes", TenantID: admin.TenantID, ProjectID: project.ID, NetworkID: network.ID, Token: "enrollment-token", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	enrollResponse := httptest.NewRecorder()
	app.Router().ServeHTTP(enrollResponse, httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", strings.NewReader(`{"token":"enrollment-token","name":"new-node","os":"linux","agent_version":"test"}`)))
	if enrollResponse.Code != http.StatusCreated {
		t.Fatalf("enroll failed: %d %s", enrollResponse.Code, enrollResponse.Body.String())
	}
	var enrolled struct {
		Node Node `json:"node"`
	}
	if err := json.NewDecoder(enrollResponse.Body).Decode(&enrolled); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	request.Header.Set("Authorization", "Bearer "+sessionToken)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("nodes failed: %d %s", response.Code, response.Body.String())
	}
	var nodes []Node
	if err := json.NewDecoder(response.Body).Decode(&nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].ID != enrolled.Node.ID || nodes[0].NetworkID != network.ID {
		t.Fatalf("enrolled node missing from node list: %#v", nodes)
	}
	if nodes[0].WireGuard == nil || len(nodes[0].WireGuard) != 0 {
		t.Fatalf("new node must be listed with an empty WireGuard array: %#v", nodes[0].WireGuard)
	}
}

func TestAllocateAddress(t *testing.T) {
	address, err := AllocateAddress("10.0.0.0/30", []string{"10.0.0.1"})
	if err != nil || address != "10.0.0.2" {
		t.Fatalf("got %s, %v", address, err)
	}
	if _, err := AllocateAddress("10.0.0.0/30", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}); err == nil {
		t.Fatal("expected exhausted pool")
	}
}

func TestTopologyCompiler(t *testing.T) {
	box, _ := NewSecretBox("test")
	makeNode := func(id, role string) Node {
		secret, _ := box.Encrypt([]byte(id + "-private"))
		return Node{ID: id, Address: "10.0.0." + id, PublicKey: id + "-public", Labels: map[string]string{"wiremesh.role": role}, PrivateKey: secret}
	}
	nodes := []Node{makeNode("1", "hub"), makeNode("2", "spoke"), makeNode("3", "spoke")}
	full, err := CompileTopology(Network{ID: "n", Topology: TopologyFullMesh}, nodes, nil, box)
	if err != nil || len(full["1"].Peers) != 2 {
		t.Fatalf("full mesh %#v %v", full, err)
	}
	hub, err := CompileTopology(Network{ID: "n", Topology: TopologyHubSpoke}, nodes, nil, box)
	if err != nil || len(hub["2"].Peers) != 1 || hub["2"].Peers[0].NodeID != "1" {
		t.Fatalf("hub spoke %#v %v", hub, err)
	}
	custom, err := CompileTopology(Network{ID: "n", Topology: TopologyCustom}, nodes, []PeerRelation{{SourceNodeID: "2", TargetNodeID: "3"}}, box)
	if err != nil || len(custom["1"].Peers) != 0 || len(custom["2"].Peers) != 1 {
		t.Fatalf("custom %#v %v", custom, err)
	}
}

func TestSecretBox(t *testing.T) {
	box, _ := NewSecretBox("key")
	secret, err := box.Encrypt([]byte("private"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := box.Decrypt(secret)
	if err != nil || string(got) != "private" {
		t.Fatalf("got %q %v", got, err)
	}
	other, _ := NewSecretBox("other")
	if _, err := other.Decrypt(secret); err == nil {
		t.Fatal("wrong master key decrypted a secret")
	}
}

func TestTenantIsolationAndRevision(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "admin@example.com", "strong-password")
	project := Project{ID: "p", TenantID: admin.TenantID, Name: "P", CreatedAt: time.Now()}
	app.store.CreateProject(project)
	network := Network{ID: "n", TenantID: admin.TenantID, ProjectID: "p", Name: "N", CIDR: "10.7.0.0/24", Topology: TopologyFullMesh}
	app.store.CreateNetwork(network)
	node, err := app.createNode(admin.TenantID, network, "node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/networks/n/publish", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("publish status %d: %s", response.Code, response.Body.String())
	}
	var revision ConfigRevision
	var published ConfigPublishResult
	_ = json.NewDecoder(response.Body).Decode(&published)
	revision, err = app.store.LatestRevision(admin.TenantID, network.ID)
	if err != nil || published.Version != 1 || revision.Version != 1 || revision.Configs[node.ID].Address == "" {
		t.Fatalf("unexpected revision %#v / %#v / %v", published, revision, err)
	}
	if _, err := app.store.GetNetwork("another_tenant", "n"); err == nil {
		t.Fatal("network leaked across tenant")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	request.Header.Set("Authorization", "Bearer bad")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatalf("expected protected nodes, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "permission") {
		t.Fatalf("unexpected error %s", response.Body.String())
	}
}

func TestNodeConfigurationIsSavedAndDeliveredImmediately(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "node-config@example.com", "strong-password")
	project := Project{ID: "project-config", TenantID: admin.TenantID, Name: "Config", CreatedAt: time.Now()}
	network := Network{ID: "network-config", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Config", CIDR: "10.55.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "node-one", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := app.createNode(admin.TenantID, network, "node-two", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/nodes/"+node.ID, strings.NewReader(`{"address":"10.55.0.20","listen_port":51821,"mtu":1380,"endpoint":"vpn.example.com:51821","location_source":"manual","location_name":"中国 上海机房"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("patch failed: %d %s", response.Code, response.Body.String())
	}
	var updated Node
	if err := json.NewDecoder(response.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Address != "10.55.0.20" || updated.ListenPort != 51821 || updated.MTU != 1380 || updated.LocationSource != "manual" || updated.LocationName != "中国 上海机房" || updated.Latitude != 31.2304 || updated.Longitude != 121.4737 {
		t.Fatalf("unexpected node: %#v", updated)
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/nodes/"+other.ID, strings.NewReader(`{"location_source":"manual"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("manual location without a name should fail: %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/nodes/"+node.ID, strings.NewReader(`{"location_source":"manual","latitude":91,"longitude":121.4737}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid latitude should fail: %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/nodes/"+node.ID, strings.NewReader(`{"address":"192.168.1.10"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range address should fail: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPatch, "/api/v1/nodes/"+node.ID, strings.NewReader(`{"address":"`+other.Address+`"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate address should conflict: %d %s", response.Code, response.Body.String())
	}

	revision, err := app.store.LatestRevision(admin.TenantID, network.ID)
	if err != nil {
		t.Fatal(err)
	}
	config := revision.Configs[node.ID]
	if revision.Version != 1 || config.Address != updated.Address || config.ListenPort != updated.ListenPort || config.MTU != updated.MTU {
		t.Fatalf("saved config was not immediately published: %#v", revision)
	}
	deliveries := app.store.ListDeliveries(admin.TenantID, "")
	if len(deliveries) != 2 {
		t.Fatalf("initial auto-publish should queue both enabled nodes, got %#v", deliveries)
	}
	if commands := app.store.ListCommands(admin.TenantID, node.ID); len(commands) == 0 || commands[0].Type != "apply_config" {
		t.Fatalf("updated node was not woken for immediate apply: %#v", commands)
	}

	now := time.Now()
	if err := app.store.UpdateDelivery(ConfigDelivery{ID: newID("delivery"), TenantID: admin.TenantID, NodeID: node.ID, Version: revision.Version, State: "applied", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpdateDelivery(ConfigDelivery{ID: newID("delivery"), TenantID: admin.TenantID, NodeID: other.ID, Version: revision.Version, State: "applied", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPatch, "/api/v1/nodes/"+node.ID, strings.NewReader(`{"listen_port":51822}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("second patch failed: %d %s", response.Code, response.Body.String())
	}
	var second struct {
		Node
		Delivery ConfigPublishResult `json:"delivery"`
	}
	if err := json.NewDecoder(response.Body).Decode(&second); err != nil {
		t.Fatal(err)
	}
	if second.Delivery.Version != 2 || len(second.Delivery.QueuedNodeIDs) != 1 || second.Delivery.QueuedNodeIDs[0] != node.ID {
		t.Fatalf("only the changed node should be queued for delivery: %#v", second.Delivery)
	}
	request = httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	request.Header.Set("X-Agent-ID", other.ID)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unchanged node should not receive the new revision: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	request.Header.Set("X-Agent-ID", node.ID)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":2`) {
		t.Fatalf("changed node should receive the new revision: %d %s", response.Code, response.Body.String())
	}
}

func TestAgentCommandsLogsAndDelete(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "node-commands@example.com", "strong-password")
	project := Project{ID: "project-commands", TenantID: admin.TenantID, Name: "Commands", CreatedAt: time.Now()}
	network := Network{ID: "network-commands", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Commands", CIDR: "10.56.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	app.store.CreateProject(project)
	app.store.CreateNetwork(network)
	node, err := app.createNode(admin.TenantID, network, "node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+node.ID+"/collect", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create command failed: %d %s", response.Code, response.Body.String())
	}
	var command AgentCommand
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		t.Fatal(err)
	}

	request = httptest.NewRequest(http.MethodGet, "/agent/v1/commands", nil)
	request.Header.Set("X-Agent-ID", node.ID)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("claim command failed: %d %s", response.Code, response.Body.String())
	}
	var commands []AgentCommand
	if err := json.NewDecoder(response.Body).Decode(&commands); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].State != "running" {
		t.Fatalf("unexpected claimed commands: %#v", commands)
	}

	request = httptest.NewRequest(http.MethodPost, "/agent/v1/commands/"+command.ID+"/result", strings.NewReader(`{"state":"completed","result":"wireguard_interfaces=1"}`))
	request.Header.Set("X-Agent-ID", node.ID)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("command result failed: %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID+"/logs", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "wireguard_interfaces=1") {
		t.Fatalf("node logs missing command result: %d %s", response.Code, response.Body.String())
	}
	var logPage NodeLogPage
	if err := json.NewDecoder(response.Body).Decode(&logPage); err != nil || len(logPage.Items) != 1 {
		t.Fatalf("unexpected paged node logs: %#v %v", logPage, err)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/"+node.ID+"/logs", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("clear node logs failed: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID+"/logs?limit=1", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "wireguard_interfaces=1") {
		t.Fatalf("node logs were not cleared: %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/"+node.ID, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete failed: %d %s", response.Code, response.Body.String())
	}
	if _, err := app.store.GetNode(admin.TenantID, node.ID); err == nil {
		t.Fatal("node still exists after delete")
	}
}

func TestAgentCommandLongPollWakesWhenCommandIsCreated(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "command-wakeup@example.com", "strong-password")
	project := Project{ID: "project-command-wakeup", TenantID: admin.TenantID, Name: "Commands", CreatedAt: time.Now()}
	network := Network{ID: "network-command-wakeup", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Commands", CIDR: "10.57.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(app.Router())
	defer server.Close()
	type result struct {
		status   int
		commands []AgentCommand
		err      error
	}
	responses := make(chan result, 1)
	go func() {
		request, err := http.NewRequest(http.MethodGet, server.URL+"/agent/v1/commands?wait=5s", nil)
		if err != nil {
			responses <- result{err: err}
			return
		}
		request.Header.Set("X-Agent-ID", node.ID)
		response, err := server.Client().Do(request)
		if err != nil {
			responses <- result{err: err}
			return
		}
		defer response.Body.Close()
		var commands []AgentCommand
		err = json.NewDecoder(response.Body).Decode(&commands)
		responses <- result{status: response.StatusCode, commands: commands, err: err}
	}()

	time.Sleep(50 * time.Millisecond)
	started := time.Now()
	command := AgentCommand{ID: "cmd-command-wakeup", TenantID: admin.TenantID, NodeID: node.ID, Type: "collect", State: "pending", CreatedAt: time.Now()}
	if err := app.createAgentCommand(command); err != nil {
		t.Fatal(err)
	}

	select {
	case response := <-responses:
		if response.err != nil {
			t.Fatal(response.err)
		}
		if response.status != http.StatusOK {
			t.Fatalf("long poll returned %d", response.status)
		}
		if len(response.commands) != 1 || response.commands[0].ID != command.ID || response.commands[0].State != "running" {
			t.Fatalf("unexpected long-poll response: %#v", response.commands)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("command wake-up took too long: %s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("long poll was not woken by the newly created command")
	}
}
