package control

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestViewerCannotExportClientConfig：L-1——viewer 不能导出含 WireGuard
// 私钥的客户端配置（提升为 operator 级），防最低权限越权。
func TestViewerCannotExportClientConfig(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "l1-admin@example.com", "strong-password")
	network := Network{ID: "net-l1", TenantID: admin.TenantID, ProjectID: "p-l1", Name: "L1", CIDR: "10.96.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(Project{ID: "p-l1", TenantID: admin.TenantID, Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "l1-node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 创建 viewer 用户
	viewer := User{ID: "l1-viewer", TenantID: admin.TenantID, Email: "l1-viewer@example.com", Name: "Viewer", Role: RoleViewer, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(viewer); err != nil {
		t.Fatal(err)
	}
	viewerToken := app.auth.issue(viewer)

	// viewer 访问 client-config → 401（withUser 对角色不足返回 401）
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID+"/client-config", nil)
	request.Header.Set("Authorization", "Bearer "+viewerToken)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("viewer must not export client config: %d %s", response.Code, response.Body.String())
	}

	// viewer 访问 peer-config → 401
	request = httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID+"/peer-config", nil)
	request.Header.Set("Authorization", "Bearer "+viewerToken)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("viewer must not export peer config: %d %s", response.Code, response.Body.String())
	}
}
