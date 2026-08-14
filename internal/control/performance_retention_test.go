package control

import (
	"testing"
	"time"
)

// TestTrafficSamplesRetentionPrunedByDays：流量采样按 retention.rawDays 清理，
// 未配置时使用默认 30 天。
func TestTrafficSamplesRetentionPrunedByDays(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "retention-traffic@example.com", "strong-password")
	now := time.Now()
	samples := []TrafficSample{
		{ID: "traffic-ret-old", TenantID: admin.TenantID, NodeID: "n", InterfaceName: "wg0", RecordedAt: now.AddDate(0, 0, -40)},
		{ID: "traffic-ret-new", TenantID: admin.TenantID, NodeID: "n", InterfaceName: "wg0", RecordedAt: now.Add(-time.Minute)},
	}
	if err := app.store.AddTrafficSamples(samples); err != nil {
		t.Fatal(err)
	}
	// 默认 30 天：40 天前的采样应被清理
	deleted, err := app.store.DeleteTrafficSamplesBefore(admin.TenantID, now.AddDate(0, 0, -defaultTrafficRetentionDays))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 pruned traffic sample, got %d", deleted)
	}
	remaining, err := app.store.ListTrafficSamples(admin.TenantID, "n", "wg0", now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != "traffic-ret-new" {
		t.Fatalf("new sample must remain: %#v", remaining)
	}
}

// TestDeliveriesRetentionKeepsNewestPerNode：下发记录按节点保留最近 N 条。
func TestDeliveriesRetentionKeepsNewestPerNode(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "retention-delivery@example.com", "strong-password")
	now := time.Now()
	for version := uint64(1); version <= maxDeliveriesPerNode+10; version++ {
		delivery := ConfigDelivery{ID: newID("delivery"), TenantID: admin.TenantID, NodeID: "node-keep", Version: version, State: "applied", UpdatedAt: now.Add(time.Duration(version) * time.Second)}
		if err := app.store.CreateDelivery(delivery); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := app.store.DeleteDeliveriesBefore(admin.TenantID, "node-keep", maxDeliveriesPerNode)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 10 {
		t.Fatalf("expected 10 pruned deliveries, got %d", deleted)
	}
	rows, err := app.store.ListDeliveries(admin.TenantID, "node-keep")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != maxDeliveriesPerNode {
		t.Fatalf("expected %d deliveries kept, got %d", maxDeliveriesPerNode, len(rows))
	}
	// 保留的是最新版本
	for _, row := range rows {
		if row.Version < 11 {
			t.Fatalf("old delivery version %d must be pruned", row.Version)
		}
	}
}

// TestRevisionsRetentionKeepsNewestVersions：网络修订保留最近 N 个版本，
// 最新版本不受影响。
func TestRevisionsRetentionKeepsNewestVersions(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "retention-revision@example.com", "strong-password")
	now := time.Now()
	for version := uint64(1); version <= maxRevisionsPerNetwork+10; version++ {
		revision := ConfigRevision{ID: newID("rev"), TenantID: admin.TenantID, ProjectID: "p", NetworkID: "net-ret", Version: version, Configs: map[string]NodeConfig{"n": {}}, CreatedAt: now}
		if err := app.store.CreateRevision(revision); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := app.store.LatestRevision(admin.TenantID, "net-ret")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != maxRevisionsPerNetwork+10 {
		t.Fatalf("latest version = %d, want %d", latest.Version, maxRevisionsPerNetwork+10)
	}
	deleted, err := app.store.DeleteRevisionsBefore(admin.TenantID, "net-ret", maxRevisionsPerNetwork+1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != maxRevisionsPerNetwork {
		t.Fatalf("expected %d pruned revisions, got %d", maxRevisionsPerNetwork, deleted)
	}
	// 最新版本依然可读
	latest, err = app.store.LatestRevision(admin.TenantID, "net-ret")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != maxRevisionsPerNetwork+10 {
		t.Fatalf("latest revision must survive pruning: %d", latest.Version)
	}
}

// TestListTenantsReturnsDistinctTenants：租户枚举去重且覆盖用户与设置来源。
func TestListTenantsReturnsDistinctTenants(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "list-tenants@example.com", "strong-password")
	other := User{ID: "tenant-user-2", TenantID: "tenant-b", Email: "tenant-b@example.com", Name: "B", Role: RoleViewer, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	tenants, err := app.store.ListTenants()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tenant := range tenants {
		seen[tenant] = true
	}
	if !seen[admin.TenantID] || !seen["tenant-b"] {
		t.Fatalf("ListTenants missing tenants: %v", tenants)
	}
}

// TestPruneOperationalDataRespectsRetentionConfig：housekeeping 的保留清理
// 尊重租户设置的 rawDays（配置 7 天时只清理 7 天前的采样）。
func TestPruneOperationalDataRespectsRetentionConfig(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "retention-config@example.com", "strong-password")
	settings, err := app.tenantSettings(admin.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	settings.Retention.RawDays = 7
	if err := app.store.UpsertSettings(settings); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	samples := []TrafficSample{
		{ID: "traffic-cfg-10d", TenantID: admin.TenantID, NodeID: "n", InterfaceName: "wg0", RecordedAt: now.AddDate(0, 0, -10)},
		{ID: "traffic-cfg-2d", TenantID: admin.TenantID, NodeID: "n", InterfaceName: "wg0", RecordedAt: now.AddDate(0, 0, -2)},
	}
	if err := app.store.AddTrafficSamples(samples); err != nil {
		t.Fatal(err)
	}
	app.pruneOperationalData()
	remaining, err := app.store.ListTrafficSamples(admin.TenantID, "n", "wg0", now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != "traffic-cfg-2d" {
		t.Fatalf("10-day sample must be pruned with 7-day retention: %#v", remaining)
	}
}

// TestUpdateNodeStatusKeepsStaticColumns：心跳状态更新不触碰静态配置
// （名称、公钥、加密私钥），只更新动态状态列。
func TestUpdateNodeStatusKeepsStaticColumns(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "node-status@example.com", "strong-password")
	network := Network{ID: "net-status", TenantID: admin.TenantID, ProjectID: "p-status", Name: "Status", CIDR: "10.92.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "status-node", "203.0.113.9:51820", "", "", "", map[string]string{"env": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	originalName, originalPublicKey, originalPrivate := node.Name, node.PublicKey, node.PrivateKey

	// 模拟心跳：只带状态字段，静态字段清零（不应被写入）
	statusOnly := node
	statusOnly.Name = ""
	statusOnly.PublicKey = ""
	statusOnly.PrivateKey = EncryptedSecret{}
	statusOnly.Hostname = "edge-status"
	statusOnly.OS = "linux/arm64"
	statusOnly.WireGuard = []WireGuardInterfaceStatus{{Name: "wg0", ListenPort: 51820, Up: true}}
	statusOnly.LastSeen = time.Now()
	if err := app.store.UpdateNodeStatus(statusOnly); err != nil {
		t.Fatal(err)
	}

	updated, err := app.store.GetNode(admin.TenantID, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != originalName {
		t.Fatalf("name must not be overwritten by status update: %q", updated.Name)
	}
	if updated.PublicKey != originalPublicKey {
		t.Fatalf("public key must not be overwritten by status update")
	}
	if updated.PrivateKey != originalPrivate {
		t.Fatal("encrypted private key must not be overwritten by status update")
	}
	if updated.Hostname != "edge-status" || updated.OS != "linux/arm64" || len(updated.WireGuard) != 1 {
		t.Fatalf("dynamic status fields were not persisted: %#v", updated)
	}
}
