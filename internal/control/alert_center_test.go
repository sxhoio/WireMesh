package control

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func hasAlertStatus(events []AlertEvent, status string) bool {
	for _, event := range events {
		if event.Status == status {
			return true
		}
	}
	return false
}

func TestAlertRuleScopeValidationAndFiltering(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "alert-scope@example.com", "strong-password")
	project := Project{ID: "scope-p", TenantID: admin.TenantID, Name: "Scope", CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	netA := Network{ID: "scope-n-a", TenantID: admin.TenantID, ProjectID: project.ID, Name: "A", CIDR: "10.60.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	netB := Network{ID: "scope-n-b", TenantID: admin.TenantID, ProjectID: project.ID, Name: "B", CIDR: "10.61.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateNetwork(netA); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(netB); err != nil {
		t.Fatal(err)
	}
	nodeA, err := app.createNode(admin.TenantID, netA, "node-a", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	nodeB, err := app.createNode(admin.TenantID, netB, "node-b", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 非法作用域类型
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules", token, `{"name":"bad","type":"node_offline","threshold_sec":300,"quiet_sec":3600,"channel_ids":[],"enabled":true,"scope_type":"region","scope_ids":[]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope_type must be rejected: %d %s", response.Code, response.Body.String())
	}
	// 作用域引用了不存在的网络
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules", token, `{"name":"bad","type":"node_offline","threshold_sec":300,"quiet_sec":3600,"channel_ids":[],"enabled":true,"scope_type":"network","scope_ids":["missing-network"]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing network scope must be rejected: %d %s", response.Code, response.Body.String())
	}
	// 网络作用域为空 id 列表
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules", token, `{"name":"bad","type":"node_offline","threshold_sec":300,"quiet_sec":3600,"channel_ids":[],"enabled":true,"scope_type":"network","scope_ids":[]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("empty network scope must be rejected: %d %s", response.Code, response.Body.String())
	}
	// 合法网络作用域
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules", token, `{"name":"scope","type":"node_offline","threshold_sec":300,"quiet_sec":3600,"channel_ids":[],"enabled":true,"scope_type":"network","scope_ids":["`+netA.ID+`"]}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("valid scoped rule must be created: %d %s", response.Code, response.Body.String())
	}
	var rule AlertRule
	if err := json.NewDecoder(response.Body).Decode(&rule); err != nil {
		t.Fatal(err)
	}
	if rule.ScopeType != "network" || len(rule.ScopeIDs) != 1 || rule.ScopeIDs[0] != netA.ID {
		t.Fatalf("unexpected scope persisted: %#v", rule)
	}
	// 默认作用域为 all
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules", token, `{"name":"default-scope","type":"node_offline","threshold_sec":300,"quiet_sec":3600,"channel_ids":[],"enabled":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("default scope rule must be created: %d %s", response.Code, response.Body.String())
	}
	_ = json.NewDecoder(response.Body).Decode(&rule)
	if rule.ScopeType != "all" {
		t.Fatalf("expected default scope all, got %q", rule.ScopeType)
	}

	// ruleScopeIncludes 单元校验
	networkScope := AlertRule{ScopeType: "network", ScopeIDs: []string{netA.ID}}
	if !app.ruleScopeIncludes(networkScope, nodeA) || app.ruleScopeIncludes(networkScope, nodeB) {
		t.Fatal("network scope must include only matching network nodes")
	}
	nodeScope := AlertRule{ScopeType: "node", ScopeIDs: []string{nodeB.ID}}
	if app.ruleScopeIncludes(nodeScope, nodeA) || !app.ruleScopeIncludes(nodeScope, nodeB) {
		t.Fatal("node scope must include only matching node")
	}
	if !app.ruleScopeIncludes(AlertRule{ScopeType: "all"}, nodeA) || !app.ruleScopeIncludes(AlertRule{ScopeType: "all"}, nodeB) {
		t.Fatal("all scope must include every node")
	}
}

func TestAlertQuietPersistsAcrossRestartAndRecovery(t *testing.T) {
	store := NewMemoryStore()
	app, err := NewApp(Config{MasterKey: "alert-key", AgentInsecureHTTP: true, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := initializeTestAdmin(t, app, "alert-quiet@example.com", "strong-password")
	network := Network{ID: "quiet-n", TenantID: admin.TenantID, ProjectID: "quiet-p", Name: "N", CIDR: "10.62.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "quiet-node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	node.LastSeen = time.Now().Add(-2 * time.Hour)
	if err := store.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	rule := AlertRule{ID: "quiet-rule", TenantID: admin.TenantID, Name: "offline", Type: alertTypeNodeOffline, ThresholdSec: 300, QuietSec: 3600, Enabled: true, ScopeType: "all", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}

	app.evaluateAlertRule(rule)
	events, _ := store.ListAlertEvents(admin.TenantID)
	if len(events) != 1 || events[0].Status != alertStatusRecorded {
		t.Fatalf("expected one recorded alert, got %#v", events)
	}

	app.evaluateAlertRule(rule)
	events, _ = store.ListAlertEvents(admin.TenantID)
	if len(events) != 1 {
		t.Fatalf("quiet period should suppress duplicate alerts, got %d", len(events))
	}

	// 模拟服务重启：同一 store 挂到新 App，静默状态必须仍然有效
	app2, err := NewApp(Config{MasterKey: "alert-key", AgentInsecureHTTP: true, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	app2.evaluateAlertRule(rule)
	events, _ = store.ListAlertEvents(admin.TenantID)
	if len(events) != 1 {
		t.Fatalf("quiet period lost across restart, got %d", len(events))
	}

	// 故障恢复：节点重新上报 → 产生恢复事件
	node.LastSeen = time.Now()
	if err := store.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	app2.evaluateAlertRule(rule)
	events, _ = store.ListAlertEvents(admin.TenantID)
	if len(events) != 2 || !hasAlertStatus(events, alertStatusRecovered) {
		t.Fatalf("expected recovery event, got %#v", events)
	}

	// 再次离线 → 重新告警（静默期只覆盖同一次故障）
	node.LastSeen = time.Now().Add(-2 * time.Hour)
	if err := store.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	app2.evaluateAlertRule(rule)
	events, _ = store.ListAlertEvents(admin.TenantID)
	if len(events) != 3 {
		t.Fatalf("expected re-alert after recovery, got %d", len(events))
	}
}

func TestAlertEventsClearAndTenantIsolation(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "alert-clear@example.com", "strong-password")
	other := User{ID: "user-other-alert", TenantID: "other-alert-tenant", Email: "other-alert@example.com", Name: "Other", Role: RoleAdmin, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	otherToken := app.auth.issue(other)
	viewer := User{ID: "viewer-alert", TenantID: admin.TenantID, Email: "viewer-alert@example.com", Name: "Viewer", Role: RoleViewer, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(viewer); err != nil {
		t.Fatal(err)
	}
	viewerToken := app.auth.issue(viewer)

	now := time.Now()
	if err := app.store.AddAlertEvent(AlertEvent{ID: "ev-a1", TenantID: admin.TenantID, RuleID: "r", RuleName: "r", NodeID: "n", NodeName: "n", Message: "a1", Status: "sent", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.AddAlertEvent(AlertEvent{ID: "ev-a2", TenantID: admin.TenantID, RuleID: "r", RuleName: "r", NodeID: "n", NodeName: "n", Message: "a2", Status: "failed", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.AddAlertEvent(AlertEvent{ID: "ev-b1", TenantID: "other-alert-tenant", RuleID: "r", RuleName: "r", NodeID: "n", NodeName: "n", Message: "b1", Status: "sent", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	response := authenticatedRequest(app, http.MethodGet, "/api/v1/settings/alert-events", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("list events: %d %s", response.Code, response.Body.String())
	}
	var page struct {
		Items   []AlertEvent `json:"items"`
		HasMore bool         `json:"has_more"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil || len(page.Items) != 2 {
		t.Fatalf("expected 2 tenant events, got %#v (%v)", page, err)
	}

	// viewer 不能清空（后端对角色不足返回 401）
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/settings/alert-events", viewerToken, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("viewer must not clear events: %d", response.Code)
	}
	// admin 清空
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/settings/alert-events", token, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("clear events: %d %s", response.Code, response.Body.String())
	}
	response = authenticatedRequest(app, http.MethodGet, "/api/v1/settings/alert-events", token, "")
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil || len(page.Items) != 0 {
		t.Fatalf("events must be cleared, got %#v (%v)", page, err)
	}
	// 其他租户不受影响
	response = authenticatedRequest(app, http.MethodGet, "/api/v1/settings/alert-events", otherToken, "")
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil || len(page.Items) != 1 || page.Items[0].ID != "ev-b1" {
		t.Fatalf("other tenant events must survive clear, got %#v (%v)", page, err)
	}
}

func TestAlertRuleEvaluateEndpoint(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "alert-eval@example.com", "strong-password")
	network := Network{ID: "eval-n", TenantID: admin.TenantID, ProjectID: "eval-p", Name: "N", CIDR: "10.63.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "eval-node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	node.LastSeen = time.Now().Add(-2 * time.Hour)
	if err := app.store.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	rule := AlertRule{ID: "eval-rule", TenantID: admin.TenantID, Name: "offline", Type: alertTypeNodeOffline, ThresholdSec: 300, QuietSec: 3600, Enabled: true, ScopeType: "all", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := app.store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}

	response := authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules/"+rule.ID+"/evaluate", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("evaluate: %d %s", response.Code, response.Body.String())
	}
	var result map[string]int
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result["evaluated"] != 1 || result["triggered"] != 1 {
		t.Fatalf("unexpected evaluate result: %#v (%v)", result, err)
	}
	// 手动评估忽略静默期，同一故障再次触发
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules/"+rule.ID+"/evaluate", token, "")
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result["triggered"] != 1 {
		t.Fatalf("manual evaluate must bypass quiet period: %#v (%v)", result, err)
	}
	events, _ := app.store.ListAlertEvents(admin.TenantID)
	if len(events) != 2 {
		t.Fatalf("expected 2 alert events from manual evaluation, got %d", len(events))
	}
	// 未知规则 404
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules/missing/evaluate", token, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown rule must 404: %d", response.Code)
	}
	// viewer 无权评估
	viewer := User{ID: "viewer-eval", TenantID: admin.TenantID, Email: "viewer-eval@example.com", Name: "Viewer", Role: RoleViewer, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(viewer); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/alert-rules/"+rule.ID+"/evaluate", app.auth.issue(viewer), "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("viewer must not evaluate: %d", response.Code)
	}
}

func TestSQLiteAlertStateAndScopePersist(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "wiremesh.db")) + "?_pragma=foreign_keys(1)"
	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "alert-sql-key", AgentInsecureHTTP: true, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := initializeTestAdmin(t, app, "alert-sql@example.com", "strong-password")
	now := time.Now()
	rule := AlertRule{ID: "rule-sql", TenantID: admin.TenantID, Name: "SQL Rule", Type: alertTypeLinkDown, ThresholdSec: 180, QuietSec: 7200, Enabled: true, ScopeType: "network", ScopeIDs: []string{"net-sql-1", "net-sql-2"}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	if err := store.AddAlertEvent(AlertEvent{ID: "ev-sql", TenantID: admin.TenantID, RuleID: rule.ID, RuleName: rule.Name, NodeID: "n", NodeName: "n", Message: "m", Status: "sent", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAlertFired(admin.TenantID, "rule-sql:node-sql"); err == nil {
		t.Fatal("missing alert state must return error")
	}
	if err := store.PutAlertFired(AlertFired{TenantID: admin.TenantID, AlertKey: "rule-sql:node-sql", FiredAt: now, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAlertFired(AlertFired{TenantID: admin.TenantID, AlertKey: "rule-sql:node-sql", FiredAt: now.Add(time.Minute), Active: false}); err != nil {
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
	rules, err := store.ListAlertRules(admin.TenantID)
	if err != nil || len(rules) != 1 {
		t.Fatalf("rule not persisted: %#v %v", rules, err)
	}
	if rules[0].ScopeType != "network" || len(rules[0].ScopeIDs) != 2 {
		t.Fatalf("scope not persisted: %#v", rules[0])
	}
	state, err := store.GetAlertFired(admin.TenantID, "rule-sql:node-sql")
	if err != nil || state.Active || !state.FiredAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("alert state not persisted: %#v %v", state, err)
	}
	if err := store.ClearAlertEvents(admin.TenantID); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListAlertEvents(admin.TenantID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events must be cleared: %#v %v", events, err)
	}
}
