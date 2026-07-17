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
		"/api/v1/audit",
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
	_ = json.NewDecoder(response.Body).Decode(&revision)
	if revision.Version != 1 || revision.Configs[node.ID].Address == "" {
		t.Fatalf("unexpected revision %#v", revision)
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
