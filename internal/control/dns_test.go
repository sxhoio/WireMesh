package control

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testDNSNetwork(t *testing.T, app *App, tenantID string) Network {
	t.Helper()
	project := Project{ID: "dns-p", TenantID: tenantID, Name: "DNS", CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	network := Network{ID: "dns-n", TenantID: tenantID, ProjectID: project.ID, Name: "DNS", CIDR: "10.80.0.0/24", DNS: "1.1.1.1", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	return network
}

func TestDNSRecordUpdateAndDuplicateConflict(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "dns-guard@example.com", "strong-password")
	network := testDNSNetwork(t, app, admin.TenantID)

	// 创建记录
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/networks/"+network.ID+"/dns-records", token, `{"name":"db.internal","address":"10.80.0.10","description":"postgres"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create record: %d %s", response.Code, response.Body.String())
	}
	var record DNSRecord
	if err := json.NewDecoder(response.Body).Decode(&record); err != nil {
		t.Fatal(err)
	}
	// 同名创建 → 409 友好中文提示（不再泄露 SQL 内部错误）
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/networks/"+network.ID+"/dns-records", token, `{"name":"DB.INTERNAL","address":"10.80.0.11"}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "同名") {
		t.Fatalf("duplicate create must 409 with friendly message: %d %s", response.Code, response.Body.String())
	}
	// 更新记录
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/dns-records/"+record.ID, token, `{"name":"db.internal","address":"10.80.0.20","description":"mysql"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("update record: %d %s", response.Code, response.Body.String())
	}
	_ = json.NewDecoder(response.Body).Decode(&record)
	if record.Address != "10.80.0.20" || record.Description != "mysql" {
		t.Fatalf("record not updated: %#v", record)
	}
	// 更新为与另一条记录同名 → 409
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/networks/"+network.ID+"/dns-records", token, `{"name":"web.internal","address":"10.80.0.30"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create second record: %d %s", response.Code, response.Body.String())
	}
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/dns-records/"+record.ID, token, `{"name":"web.internal","address":"10.80.0.20"}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "同名") {
		t.Fatalf("duplicate update must 409 with friendly message: %d %s", response.Code, response.Body.String())
	}
	// 未知记录 404
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/dns-records/missing", token, `{"name":"x.internal","address":"10.80.0.50"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("update missing record must 404: %d", response.Code)
	}
	// 非法 IP / 非法名称 400
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/dns-records/"+record.ID, token, `{"name":"db.internal","address":"not-an-ip"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid address must 400: %d", response.Code)
	}
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/dns-records/"+record.ID, token, `{"name":"bad name","address":"10.80.0.20"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid name must 400: %d", response.Code)
	}

	// viewer 无权更新（后端对角色不足返回 401）
	viewer := User{ID: "viewer-dns", TenantID: admin.TenantID, Email: "viewer-dns@example.com", Name: "Viewer", Role: RoleViewer, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(viewer); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/dns-records/"+record.ID, app.auth.issue(viewer), `{"name":"db.internal","address":"10.80.0.20"}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("viewer must not update: %d", response.Code)
	}
	// 其他租户不可见 → 404
	other := User{ID: "other-dns", TenantID: "other-dns-tenant", Email: "other-dns@example.com", Name: "Other", Role: RoleAdmin, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/dns-records/"+record.ID, app.auth.issue(other), `{"name":"db.internal","address":"10.80.0.20"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant update must 404: %d", response.Code)
	}
}
