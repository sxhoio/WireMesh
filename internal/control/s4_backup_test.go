package control

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRestorePersistsAcrossRestart：恢复后写入的数据在进程重启后仍然存在
// （活动库指向持久化路径，而非可删除的临时文件）。
func TestRestorePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "wiremesh-database.json")
	dbPath := filepath.Join(dir, "wiremesh.db")

	// 初始化一个带管理员与项目的数据源，并生成备份文件
	manager, err := NewDatabaseManager(configPath, "s4-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, _, err := manager.Configure(t.Context(), DatabaseConfig{Driver: "sqlite", SQLitePath: dbPath}); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "s4-key", Store: manager.Store(), Database: manager})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := initializeTestAdmin(t, app, "s4-admin@example.com", "strong-password")
	project := Project{ID: "s4-p", TenantID: admin.TenantID, Name: "S4", CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "backup.db")
	if err := manager.BackupSQLite(backupPath); err != nil {
		t.Fatal(err)
	}

	// 热切换恢复（活动库将被替换为备份内容）
	if err := manager.RestoreSQLite(t.Context(), backupPath); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// 恢复后数据可见，并写入一条新数据
	if _, err := app.store.GetProject(admin.TenantID, project.ID); err != nil {
		t.Fatalf("restored project must be visible: %v", err)
	}
	if err := app.store.CreateProject(Project{ID: "s4-post", TenantID: admin.TenantID, Name: "Post", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// 模拟重启：全新 manager 从同一配置/路径加载，恢复与后续写入都应仍在
	manager3, err := NewDatabaseManager(configPath, "s4-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager3.Close()
	app3, err := NewApp(Config{MasterKey: "s4-key", Store: manager3.Store(), Database: manager3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app3.store.GetProject(admin.TenantID, project.ID); err != nil {
		t.Fatalf("restored project must survive restart: %v", err)
	}
	if _, err := app3.store.GetProject(admin.TenantID, "s4-post"); err != nil {
		t.Fatalf("post-restore writes must survive restart: %v", err)
	}
}

// TestRestoreRejectsInvalidBackups：无用户 / 非 SQLite 文件的备份被拒绝，
// 且当前库保持不变。
func TestRestoreRejectsInvalidBackups(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewDatabaseManager(filepath.Join(dir, "config.json"), "s4-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, _, err := manager.Configure(t.Context(), DatabaseConfig{Driver: "sqlite", SQLitePath: filepath.Join(dir, "wiremesh.db")}); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "s4-key", Store: manager.Store(), Database: manager})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := initializeTestAdmin(t, app, "s4-invalid@example.com", "strong-password")

	// 非 SQLite 文件
	badPath := filepath.Join(dir, "not-sqlite.db")
	if err := os.WriteFile(badPath, []byte("this is not a sqlite file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.RestoreSQLite(t.Context(), badPath); err == nil {
		t.Fatal("non-sqlite file must be rejected")
	}
	// 无用户的合法 SQLite（仅创建文件）
	emptyPath := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(emptyPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.RestoreSQLite(t.Context(), emptyPath); err == nil {
		t.Fatal("backup without users must be rejected")
	}
	// 拒绝后当前库仍可用
	if _, err := app.store.GetUser(admin.ID); err != nil {
		t.Fatalf("current database must remain usable after rejected restore: %v", err)
	}
}
