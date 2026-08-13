package control

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testAccessNetwork(t *testing.T, app *App, tenantID string) (Network, Node, Node) {
	t.Helper()
	project := Project{ID: "access-p", TenantID: tenantID, Name: "Access", CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	network := Network{ID: "access-n", TenantID: tenantID, ProjectID: project.ID, Name: "Access", CIDR: "10.70.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	nodeOne, err := app.createNode(tenantID, network, "access-one", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	nodeTwo, err := app.createNode(tenantID, network, "access-two", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return network, nodeOne, nodeTwo
}

func TestAccessResourceUpdateAndReferenceGuard(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "access-guard@example.com", "strong-password")
	network, nodeOne, _ := testAccessNetwork(t, app, admin.TenantID)

	// 创建资源
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/networks/"+network.ID+"/access-resources", token, `{"name":"DB","gateway_node_id":"`+nodeOne.ID+`","target":"10.99.0.0/24","port":5432,"protocol":"tcp","description":"postgres"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create resource: %d %s", response.Code, response.Body.String())
	}
	var resource AccessResource
	if err := json.NewDecoder(response.Body).Decode(&resource); err != nil {
		t.Fatal(err)
	}
	// 更新资源
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/access-resources/"+resource.ID, token, `{"name":"DB2","gateway_node_id":"`+nodeOne.ID+`","target":"10.99.1.0/24","port":3306,"protocol":"any","description":"mysql"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("update resource: %d %s", response.Code, response.Body.String())
	}
	_ = json.NewDecoder(response.Body).Decode(&resource)
	if resource.Name != "DB2" || resource.Target != "10.99.1.0/24" || resource.Port != 3306 {
		t.Fatalf("resource not updated: %#v", resource)
	}
	// 未知资源 404
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/networks/"+network.ID+"/access-resources/missing", token, `{"name":"X","gateway_node_id":"`+nodeOne.ID+`","target":"10.99.0.0/24"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("update missing resource must 404: %d", response.Code)
	}

	// 创建引用该资源的策略
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/networks/"+network.ID+"/access-policies", token, `{"name":"DB Policy","source_label":"","source_node_ids":[],"resource_ids":["`+resource.ID+`"],"enabled":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create policy: %d %s", response.Code, response.Body.String())
	}
	var policy AccessPolicy
	if err := json.NewDecoder(response.Body).Decode(&policy); err != nil {
		t.Fatal(err)
	}
	// 被引用时删除 → 409 并列出策略名
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/networks/"+network.ID+"/access-resources/"+resource.ID, token, "")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "DB Policy") {
		t.Fatalf("referenced delete must 409 with policy name: %d %s", response.Code, response.Body.String())
	}
	// 删除策略后可删除资源
	if err := app.store.DeleteAccessPolicy(admin.TenantID, policy.ID); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/networks/"+network.ID+"/access-resources/"+resource.ID, token, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete resource after policy removal: %d %s", response.Code, response.Body.String())
	}
}

func TestAccessPolicyEmptySourceAppliesToAllNodes(t *testing.T) {
	nodes := []Node{
		{ID: "src-a", Address: "10.0.0.2", Labels: map[string]string{}},
		{ID: "src-b", Address: "10.0.0.3", Labels: map[string]string{"team": "ops"}},
		{ID: "gw", Address: "10.0.0.4", Labels: map[string]string{}},
	}
	network := Network{ID: "allow-n", TenantID: "t", Topology: TopologyFullMesh, CIDR: "10.0.0.0/24"}
	resource := AccessResource{ID: "res", TenantID: "t", NetworkID: network.ID, Name: "svc", GatewayNodeID: "gw", Target: "10.77.0.0/24"}
	resources := []AccessResource{resource}

	// 空源策略 → 全部节点（除网关自身）
	policy := AccessPolicy{ID: "p-all", TenantID: "t", NetworkID: network.ID, Name: "all", SourceNodeIDs: []string{}, ResourceIDs: []string{"res"}, Enabled: true}
	out := compileAccessAllowlist(network, nodes, resources, []AccessPolicy{policy})
	if len(out["src-a\x00gw"]) != 1 || len(out["src-b\x00gw"]) != 1 {
		t.Fatalf("empty-source policy must apply to all nodes, got %#v", out)
	}
	// 网关自身不生成自指路由
	if len(out["gw\x00gw"]) != 0 {
		t.Fatalf("gateway must not route to itself, got %#v", out)
	}

	// 标签选择器只匹配 team=ops
	labeled := AccessPolicy{ID: "p-label", TenantID: "t", NetworkID: network.ID, Name: "ops", SourceLabel: "team=ops", SourceNodeIDs: []string{}, ResourceIDs: []string{"res"}, Enabled: true}
	out = compileAccessAllowlist(network, nodes, resources, []AccessPolicy{labeled})
	if len(out["src-b\x00gw"]) != 1 || len(out["src-a\x00gw"]) != 0 {
		t.Fatalf("label policy must match only labeled nodes, got %#v", out)
	}

	// 停用策略不生效
	disabled := AccessPolicy{ID: "p-off", TenantID: "t", NetworkID: network.ID, Name: "off", SourceNodeIDs: []string{}, ResourceIDs: []string{"res"}, Enabled: false}
	out = compileAccessAllowlist(network, nodes, resources, []AccessPolicy{disabled})
	if len(out) != 0 {
		t.Fatalf("disabled policy must not apply, got %#v", out)
	}
}

func TestNodeDeleteCascadesAccessResources(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "access-cascade@example.com", "strong-password")
	network, nodeOne, nodeTwo := testAccessNetwork(t, app, admin.TenantID)

	response := authenticatedRequest(app, http.MethodPost, "/api/v1/networks/"+network.ID+"/access-resources", token, `{"name":"OnTwo","gateway_node_id":"`+nodeTwo.ID+`","target":"10.88.0.0/24"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create resource: %d %s", response.Code, response.Body.String())
	}
	var resource AccessResource
	if err := json.NewDecoder(response.Body).Decode(&resource); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/networks/"+network.ID+"/access-policies", token, `{"name":"Cascade Policy","source_node_ids":["`+nodeOne.ID+`"],"resource_ids":["`+resource.ID+`"],"enabled":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create policy: %d %s", response.Code, response.Body.String())
	}
	var policy AccessPolicy
	if err := json.NewDecoder(response.Body).Decode(&policy); err != nil {
		t.Fatal(err)
	}

	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/nodes/"+nodeTwo.ID, token, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete node: %d %s", response.Code, response.Body.String())
	}
	resources, err := app.store.ListAccessResources(admin.TenantID, network.ID)
	if err != nil || len(resources) != 0 {
		t.Fatalf("gateway resources must be removed with node: %#v %v", resources, err)
	}
	policies, err := app.store.ListAccessPolicies(admin.TenantID, network.ID)
	if err != nil || len(policies) != 1 || len(policies[0].ResourceIDs) != 0 {
		t.Fatalf("policy references must be cleaned: %#v %v", policies, err)
	}
}
