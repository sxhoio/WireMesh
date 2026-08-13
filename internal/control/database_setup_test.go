package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseSetupSQLiteFlow(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "wiremesh-database.json")
	manager, err := NewDatabaseManager(configPath, "database-setup-test-key")
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "database-setup-test-key", Store: manager.Store(), Database: manager})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Router()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"database_configured":false`) {
		t.Fatalf("unexpected database setup status: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{"email":"admin@example.com","name":"Administrator","password":"strong-password"}`)))
	if response.Code != http.StatusConflict {
		t.Fatalf("administrator setup must require a database: %d %s", response.Code, response.Body.String())
	}

	payload := `{"driver":"sqlite","sqlite_path":"data/wiremesh.db"}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup/database/test", strings.NewReader(payload)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"connected":true`) {
		t.Fatalf("SQLite connection test failed: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup/database", strings.NewReader(payload)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"configured":true`) {
		t.Fatalf("SQLite configuration failed: %d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("database configuration was not persisted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "data", "wiremesh.db")); err != nil {
		t.Fatalf("SQLite database was not created: %v", err)
	}

	var tableCount int
	if err := manager.active.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('users', 'projects', 'networks', 'nodes', 'peer_relations', 'config_revisions', 'config_deliveries', 'enrollment_tokens', 'agent_identities', 'audit_events', 'setup_locks', 'system_settings', 'notification_channels', 'notification_logs')`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 14 {
		t.Fatalf("expected all WireMesh tables, found %d", tableCount)
	}

	initializeTestAdmin(t, app, "database-admin@example.com", "strong-password")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup/database", strings.NewReader(payload)))
	if response.Code != http.StatusConflict {
		t.Fatalf("initialized instance must reject database changes: %d %s", response.Code, response.Body.String())
	}

	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewDatabaseManager(configPath, "database-setup-test-key")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if status := restarted.Status(); !status.Configured || status.Driver != "sqlite" {
		t.Fatalf("saved database was not restored: %#v", status)
	}
	initialized, err := restarted.Store().HasUsers()
	if err != nil || !initialized {
		t.Fatalf("administrator did not survive database manager restart: initialized=%v err=%v", initialized, err)
	}
}

func TestDatabaseSetupRejectsInvalidSQLitePath(t *testing.T) {
	directory := t.TempDir()
	manager, err := NewDatabaseManager(filepath.Join(directory, "database.json"), "test-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	app, err := NewApp(Config{MasterKey: "test-key", Store: manager.Store(), Database: manager})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(DatabaseConfig{Driver: "sqlite", SQLitePath: directory})
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup/database", strings.NewReader(string(body))))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "文件") {
		t.Fatalf("unexpected invalid SQLite path response: %d %s", response.Code, response.Body.String())
	}
}

func TestDatabaseConfigurationIsEncrypted(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "database.json")
	manager, err := NewDatabaseManager(configPath, "encryption-test-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := DatabaseConfig{Driver: "mysql", Host: "db.internal", Port: 3306, Database: "wiremesh", Username: "wiremesh_user", Password: "top-secret-password", SSLMode: "require"}
	if err := manager.save(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{cfg.Password, cfg.Username, cfg.Host, cfg.Database} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("stored bootstrap configuration leaked %q: %s", secret, raw)
		}
	}
	if strings.Contains(string(raw), "Passwd") || strings.Contains(string(raw), "password") {
		t.Fatalf("stored bootstrap configuration exposed password fields: %s", raw)
	}
}

func TestDatabaseDSNAndMySQLSchema(t *testing.T) {
	base := t.TempDir()
	mysqlConfig, mysqlDSN, err := normalizeDatabaseConfig(DatabaseConfig{Driver: "mysql", Host: "localhost", Port: 3306, Database: "wiremesh", Username: "user", Password: "p@ss/word", SSLMode: "require"}, base)
	if err != nil {
		t.Fatal(err)
	}
	if mysqlConfig.Driver != "mysql" || !strings.Contains(mysqlDSN, "tcp(localhost:3306)") || !strings.Contains(mysqlDSN, "tls=true") {
		t.Fatalf("unexpected MySQL DSN: %s", mysqlDSN)
	}
	postgresConfig, postgresDSN, err := normalizeDatabaseConfig(DatabaseConfig{Driver: "pgsql", Host: "localhost", Database: "wiremesh", Username: "user", Password: "secret", SSLMode: "verify-full"}, base)
	if err != nil {
		t.Fatal(err)
	}
	if postgresConfig.Driver != "postgres" || postgresConfig.Port != 5432 || !strings.Contains(postgresDSN, "sslmode=verify-full") {
		t.Fatalf("unexpected PostgreSQL DSN: %s", postgresDSN)
	}
	for _, statement := range mysqlSchemaStatements() {
		if strings.Contains(statement, "CREATE INDEX IF NOT EXISTS") || strings.Contains(statement, " TEXT PRIMARY KEY") {
			t.Fatalf("MySQL schema contains incompatible SQL: %s", statement)
		}
	}
	store := &SQLStore{driver: "mysql"}
	if got := store.query("SELECT * FROM users WHERE email = ?"); got != "SELECT * FROM users WHERE email = ?" {
		t.Fatalf("MySQL placeholders must remain question marks: %s", got)
	}
}

func TestDatabaseConnectionValidation(t *testing.T) {
	manager, err := NewDatabaseManager(filepath.Join(t.TempDir(), "database.json"), "test-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := manager.Test(context.Background(), DatabaseConfig{Driver: "mysql"}); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected MySQL host validation error, got %v", err)
	}
}
