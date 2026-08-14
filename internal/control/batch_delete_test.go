package control

import (
	"path/filepath"
	"testing"
	"time"
)

// TestDeleteTrafficSamplesBatchedSQLite：SQLite 下分批删除大表完整清理
// （每批 500 行循环），验证 LIMIT 分批路径可用且不丢数据。
func TestDeleteTrafficSamplesBatchedSQLite(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewDatabaseManager(filepath.Join(dir, "config.json"), "batch-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, _, err := manager.Configure(t.Context(), DatabaseConfig{Driver: "sqlite", SQLitePath: filepath.Join(dir, "wiremesh.db")}); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "batch-key", AgentInsecureHTTP: true, Store: manager.Store(), Database: manager})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := initializeTestAdmin(t, app, "batch-traffic@example.com", "strong-password")
	now := time.Now()
	// 写入 1200 条早于保留期的采样（>2 批，触发循环）
	samples := make([]TrafficSample, 0, 1200)
	for i := 0; i < 1200; i++ {
		samples = append(samples, TrafficSample{ID: "ts-" + itoa(i), TenantID: admin.TenantID, NodeID: "batch-node", RecordedAt: now.Add(-48 * time.Hour), ReceiveBytes: 100, TransmitBytes: 200})
	}
	if err := app.store.AddTrafficSamples(samples); err != nil {
		t.Fatalf("add samples: %v", err)
	}
	deleted, err := app.store.DeleteTrafficSamplesBefore(admin.TenantID, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("batched delete: %v", err)
	}
	if deleted != 1200 {
		t.Fatalf("expected 1200 deleted, got %d", deleted)
	}
	// 近期采样不受影响
	recent := make([]TrafficSample, 0, 5)
	for i := 0; i < 5; i++ {
		recent = append(recent, TrafficSample{ID: "recent-" + itoa(i), TenantID: admin.TenantID, NodeID: "batch-node", RecordedAt: now.Add(-time.Hour), ReceiveBytes: 1, TransmitBytes: 2})
	}
	if err := app.store.AddTrafficSamples(recent); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.DeleteTrafficSamplesBefore(admin.TenantID, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	remaining, err := app.store.ListTrafficSamples(admin.TenantID, "batch-node", "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 5 {
		t.Fatalf("expected 5 recent samples remaining, got %d", len(remaining))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
