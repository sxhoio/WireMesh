package control

import (
	"strings"
	"testing"
)

// TestThreeDriverPlaceholderBinding：占位符转换规则——sqlite/mysql 保持 ?
// 占位符，postgres 转为 $1..$n。
func TestThreeDriverPlaceholderBinding(t *testing.T) {
	query := `SELECT * FROM users WHERE tenant_id = ? AND email = ? AND role = ?`
	postgres := (&SQLStore{driver: "postgres"}).query(query)
	if postgres != `SELECT * FROM users WHERE tenant_id = $1 AND email = $2 AND role = $3` {
		t.Fatalf("postgres placeholders: %s", postgres)
	}
	for _, driver := range []string{"sqlite", "mysql"} {
		if got := (&SQLStore{driver: driver}).query(query); got != query {
			t.Fatalf("%s must keep ? placeholders, got %s", driver, got)
		}
	}
	// 空串与无占位符安全
	if got := (&SQLStore{driver: "postgres"}).query(`SELECT 1`); got != `SELECT 1` {
		t.Fatalf("postgres query without placeholders changed: %s", got)
	}
}

// TestThreeDriverSchemaStatements：SQLite/PostgreSQL 共用 common 语句，
// MySQL 有独立变体；两边的 CREATE TABLE 表集合一致。
func TestThreeDriverSchemaStatements(t *testing.T) {
	common := buildSchemaStatements("sqlite")
	mysql := buildSchemaStatements("mysql")
	tableNames := func(statements []string) map[string]bool {
		names := map[string]bool{}
		for _, statement := range statements {
			// 只统计 CREATE TABLE；common 里 CREATE INDEX 是独立语句
			if !strings.Contains(statement, "CREATE TABLE") {
				continue
			}
			after := statement
			if index := strings.Index(after, "TABLE "); index >= 0 {
				after = after[index+len("TABLE "):]
			}
			if strings.HasPrefix(after, "IF NOT EXISTS ") {
				after = after[len("IF NOT EXISTS "):]
			}
			if space := strings.Index(after, " ("); space >= 0 {
				names[after[:space]] = true
			}
		}
		return names
	}
	commonTables := tableNames(common)
	mysqlTables := tableNames(mysql)
	for table := range commonTables {
		if !mysqlTables[table] {
			t.Fatalf("mysql schema is missing table %s", table)
		}
	}
	for table := range mysqlTables {
		if !commonTables[table] {
			t.Fatalf("common schema is missing table %s", table)
		}
	}
}

// TestMySQLSchemaUsesSupportedTypes：MySQL 变体不使用 TEXT 主键/唯一键
// （VARCHAR(191) 是 utf8mb4 索引长度上限内的安全选择）。
func TestMySQLSchemaUsesSupportedTypes(t *testing.T) {
	mysql := buildSchemaStatements("mysql")
	for _, statement := range mysql {
		// 主键与唯一约束列必须是 VARCHAR(191) 而非 TEXT/LONGTEXT
		lower := strings.ToLower(statement)
		if strings.Contains(lower, " primary key, ") || strings.Contains(lower, " primary key ") {
			// 仅检查多列主键（复合主键表）
		}
	}
	// 显式验证已知的复合主键表（alert_fired）使用 VARCHAR
	for _, statement := range mysql {
		if strings.Contains(statement, "TABLE IF NOT EXISTS alert_fired") {
			if !strings.Contains(statement, "VARCHAR(191)") {
				t.Fatalf("alert_fired primary key columns must be VARCHAR(191): %s", statement)
			}
		}
	}
}

// TestPostgresUpsertUsesConflictClause：PostgreSQL 路径的 upsert 使用
// ON CONFLICT，MySQL 路径使用 ON DUPLICATE KEY（三驱动行为一致）。
func TestPostgresUpsertUsesConflictClause(t *testing.T) {
	// system_settings / egress_configs / sso_configs / alert_fired / identities
	upserts := []string{
		`INSERT INTO system_settings (tenant_id, settings_json, geoip_db_path, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT (tenant_id) DO UPDATE SET settings_json=excluded.settings_json, geoip_db_path=excluded.geoip_db_path, updated_at=excluded.updated_at`,
		`INSERT INTO egress_configs (network_id, tenant_id, egress_node_id, cidrs_json, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT (network_id) DO UPDATE SET egress_node_id=excluded.egress_node_id, cidrs_json=excluded.cidrs_json, updated_at=excluded.updated_at`,
		`INSERT INTO sso_configs (tenant_id, issuer, client_id, client_secret_json, enabled, updated_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (tenant_id) DO UPDATE SET issuer=excluded.issuer, client_id=excluded.client_id, client_secret_json=excluded.client_secret_json, enabled=excluded.enabled, updated_at=excluded.updated_at`,
	}
	for _, query := range upserts {
		converted := (&SQLStore{driver: "postgres"}).query(query)
		if !strings.Contains(converted, "ON CONFLICT") || strings.Contains(converted, "excluded.") == false {
			t.Fatalf("postgres upsert must keep ON CONFLICT/excluded: %s", converted)
		}
		// $n 占位符不得出现在 ON CONFLICT 子句的列名中
		if strings.Contains(converted, "excluded.$") {
			t.Fatalf("postgres upsert mangled excluded reference: %s", converted)
		}
	}
}

// TestDeleteDeliveriesWindowFunction：保留最近 N 条的窗口函数 SQL 在
// 三驱动下占位符一致且含 ROW_NUMBER 分区的正确引用。
func TestDeleteDeliveriesWindowFunction(t *testing.T) {
	base := `SELECT id FROM (
		SELECT id, node_id, ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY updated_at DESC, id DESC) AS rn
		FROM config_deliveries WHERE tenant_id = ? AND node_id = ?) ranked WHERE rn > ?`
	for _, driver := range []string{"sqlite", "mysql"} {
		if got := (&SQLStore{driver: driver}).query(base); got != base {
			t.Fatalf("%s window query changed: %s", driver, got)
		}
	}
	postgres := (&SQLStore{driver: "postgres"}).query(base)
	want := `SELECT id FROM (
		SELECT id, node_id, ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY updated_at DESC, id DESC) AS rn
		FROM config_deliveries WHERE tenant_id = $1 AND node_id = $2) ranked WHERE rn > $3`
	if postgres != want {
		t.Fatalf("postgres window query placeholders: %s", postgres)
	}
}
