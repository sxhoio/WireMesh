package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHeartbeatCannotOverwriteLabels：H-1——Agent 心跳提交的标签不再覆写
// 节点标签（wiremesh.role 等管理字段只能由控制台维护）。
func TestHeartbeatCannotOverwriteLabels(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "h1-heartbeat@example.com", "strong-password")
	network := Network{ID: "net-h1", TenantID: admin.TenantID, ProjectID: "p-h1", Name: "H1", CIDR: "10.93.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(Project{ID: "p-h1", TenantID: admin.TenantID, Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	// 控制台设置管理标签
	node, err := app.createNode(admin.TenantID, network, "h1-node", "", "", "", "", map[string]string{"wiremesh.role": "spoke", "team": "ops"})
	if err != nil {
		t.Fatal(err)
	}
	// 恶意心跳尝试自授 hub 角色 + 伪造策略标签
	payload := `{"labels":{"wiremesh.role":"hub","team":"malicious"},"os":"linux/amd64"}`
	request := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(payload))
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat: %d %s", response.Code, response.Body.String())
	}
	updated, err := app.store.GetNode(admin.TenantID, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Labels["wiremesh.role"] != "spoke" {
		t.Fatalf("heartbeat must not overwrite wiremesh.role: %v", updated.Labels)
	}
	if updated.Labels["team"] != "ops" {
		t.Fatalf("heartbeat must not overwrite custom labels: %v", updated.Labels)
	}
}

// TestEnrollStripsReservedLabels：H-1——注册时 Agent 自报的 wiremesh.* 管理
// 标签被剥离，不能用于自授拓扑角色。
func TestEnrollStripsReservedLabels(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "h1-enroll@example.com", "strong-password")
	project := Project{ID: "p-h1-enroll", TenantID: admin.TenantID, Name: "P", CreatedAt: time.Now()}
	network := Network{ID: "net-h1-enroll", TenantID: admin.TenantID, ProjectID: project.ID, Name: "N", CIDR: "10.94.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateEnrollment(EnrollmentToken{ID: "enroll-h1", TenantID: admin.TenantID, ProjectID: project.ID, NetworkID: network.ID, Token: "h1-enroll-token", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	body := `{"token":"h1-enroll-token","name":"h1-node","os":"linux/amd64","agent_version":"0.3.7","labels":{"wiremesh.role":"hub","env":"prod"}}`
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("enroll: %d %s", response.Code, response.Body.String())
	}
	var enrolled struct {
		Node Node `json:"node"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	node, err := app.store.GetNode(admin.TenantID, enrolled.Node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if node.Labels["wiremesh.role"] != "" {
		t.Fatalf("enroll must strip reserved wiremesh.role label: %v", node.Labels)
	}
	if node.Labels["env"] != "prod" {
		t.Fatalf("enroll must keep custom labels: %v", node.Labels)
	}
}
