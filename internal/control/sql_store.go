package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type SQLStore struct {
	db     *sql.DB
	driver string
}

func OpenSQLStore(driver, dsn string) (*SQLStore, error) {
	driver = strings.ToLower(strings.TrimSpace(driver))
	sqlDriver := ""
	switch driver {
	case "sqlite", "sqlite3":
		driver, sqlDriver = "sqlite", "sqlite"
	case "postgres", "postgresql", "pgsql", "pgx":
		driver, sqlDriver = "postgres", "pgx"
	case "mysql":
		driver, sqlDriver = "mysql", "mysql"
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	db, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	store := &SQLStore{db: db, driver: driver}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLStore) Close() error   { return s.db.Close() }
func (s *SQLStore) Driver() string { return s.driver }

func commonSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS setup_locks (name TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL, last_login_at TEXT, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS projects_tenant_idx ON projects (tenant_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS networks (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, name TEXT NOT NULL, cidr TEXT NOT NULL, dns TEXT NOT NULL, topology TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS networks_project_idx ON networks (tenant_id, project_id)`,
		`CREATE TABLE IF NOT EXISTS nodes (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, network_id TEXT NOT NULL, name TEXT NOT NULL, address TEXT NOT NULL, endpoint TEXT NOT NULL, region TEXT NOT NULL, os TEXT NOT NULL, agent_version TEXT NOT NULL, labels_json TEXT NOT NULL, public_key TEXT NOT NULL, private_key_json TEXT NOT NULL, wireguard_json TEXT NOT NULL, last_seen TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS nodes_network_idx ON nodes (tenant_id, network_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS nodes_address_idx ON nodes (network_id, address)`,
		`CREATE TABLE IF NOT EXISTS peer_relations (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, network_id TEXT NOT NULL, source_node_id TEXT NOT NULL, target_node_id TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS peers_network_idx ON peer_relations (tenant_id, network_id)`,
		`CREATE TABLE IF NOT EXISTS config_revisions (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, network_id TEXT NOT NULL, version BIGINT NOT NULL, configs_json TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE (network_id, version))`,
		`CREATE TABLE IF NOT EXISTS config_deliveries (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, node_id TEXT NOT NULL, version BIGINT NOT NULL, state TEXT NOT NULL, message TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE (tenant_id, node_id, version))`,
		`CREATE INDEX IF NOT EXISTS deliveries_tenant_idx ON config_deliveries (tenant_id, updated_at)`,
		`CREATE TABLE IF NOT EXISTS enrollment_tokens (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, network_id TEXT NOT NULL, token TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, used_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS agent_identities (node_id TEXT PRIMARY KEY, certificate_pem TEXT NOT NULL, certificate_fingerprint TEXT NOT NULL, expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS system_settings (tenant_id TEXT PRIMARY KEY, settings_json TEXT NOT NULL, geoip_db_path TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS notification_channels (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, name TEXT NOT NULL, type TEXT NOT NULL, target_json TEXT NOT NULL, enabled INTEGER NOT NULL, all_agents INTEGER NOT NULL, agent_ids_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS notification_channels_tenant_idx ON notification_channels (tenant_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS notification_logs (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, channel_id TEXT NOT NULL, channel_name TEXT NOT NULL, channel_type TEXT NOT NULL, agent_name TEXT NOT NULL, message TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS notification_logs_tenant_idx ON notification_logs (tenant_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS audit_events (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, metadata_json TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS audit_tenant_idx ON audit_events (tenant_id, created_at)`,
	}
}

func mysqlSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS setup_locks (name VARCHAR(191) PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS users (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, email VARCHAR(320) NOT NULL UNIQUE, password_hash VARCHAR(255) NOT NULL, name VARCHAR(255) NOT NULL, role VARCHAR(32) NOT NULL, last_login_at VARCHAR(40), created_at VARCHAR(40) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS projects (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, description TEXT NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX projects_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS networks (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, cidr VARCHAR(64) NOT NULL, dns TEXT NOT NULL, topology VARCHAR(32) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX networks_project_idx (tenant_id, project_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS nodes (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, address VARCHAR(64) NOT NULL, endpoint TEXT NOT NULL, region VARCHAR(255) NOT NULL, os VARCHAR(255) NOT NULL, agent_version VARCHAR(255) NOT NULL, labels_json LONGTEXT NOT NULL, public_key TEXT NOT NULL, private_key_json LONGTEXT NOT NULL, wireguard_json LONGTEXT NOT NULL, last_seen VARCHAR(40) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX nodes_network_idx (tenant_id, network_id), UNIQUE INDEX nodes_address_idx (network_id, address)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS peer_relations (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, source_node_id VARCHAR(191) NOT NULL, target_node_id VARCHAR(191) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX peers_network_idx (tenant_id, network_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS config_revisions (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, version BIGINT NOT NULL, configs_json LONGTEXT NOT NULL, created_at VARCHAR(40) NOT NULL, UNIQUE INDEX revisions_network_version_idx (network_id, version)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS config_deliveries (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, node_id VARCHAR(191) NOT NULL, version BIGINT NOT NULL, state VARCHAR(64) NOT NULL, message TEXT NOT NULL, updated_at VARCHAR(40) NOT NULL, UNIQUE INDEX deliveries_node_version_idx (tenant_id, node_id, version), INDEX deliveries_tenant_idx (tenant_id, updated_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS enrollment_tokens (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, token VARCHAR(255) NOT NULL UNIQUE, expires_at VARCHAR(40) NOT NULL, used_at VARCHAR(40) NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS agent_identities (node_id VARCHAR(191) PRIMARY KEY, certificate_pem LONGTEXT NOT NULL, certificate_fingerprint VARCHAR(255) NOT NULL, expires_at VARCHAR(40) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS system_settings (tenant_id VARCHAR(191) PRIMARY KEY, settings_json LONGTEXT NOT NULL, geoip_db_path TEXT NOT NULL, updated_at VARCHAR(40) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS notification_channels (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, type VARCHAR(32) NOT NULL, target_json LONGTEXT NOT NULL, enabled BOOLEAN NOT NULL, all_agents BOOLEAN NOT NULL, agent_ids_json LONGTEXT NOT NULL, created_at VARCHAR(40) NOT NULL, updated_at VARCHAR(40) NOT NULL, INDEX notification_channels_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS notification_logs (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, channel_id VARCHAR(191) NOT NULL, channel_name VARCHAR(255) NOT NULL, channel_type VARCHAR(32) NOT NULL, agent_name VARCHAR(255) NOT NULL, message TEXT NOT NULL, status VARCHAR(32) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX notification_logs_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS audit_events (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, actor_id VARCHAR(191) NOT NULL, action VARCHAR(255) NOT NULL, resource_type VARCHAR(255) NOT NULL, resource_id VARCHAR(191) NOT NULL, metadata_json LONGTEXT NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX audit_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
}

func (s *SQLStore) migrate(ctx context.Context) error {
	statements := commonSchemaStatements()
	if s.driver == "mysql" {
		statements = mysqlSchemaStatements()
		for _, statement := range statements {
			if _, err := s.db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("database migration: %w", err)
			}
		}
	} else {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("database migration: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if err := s.ensureUserLastLoginColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureSystemSettingsGeoIPColumn(ctx); err != nil {
		return err
	}
	return s.ensureNodeWireGuardColumn(ctx)
}

func (s *SQLStore) ensureUserLastLoginColumn(ctx context.Context) error {
	var count int
	var lookup string
	switch s.driver {
	case "sqlite":
		lookup = `SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'last_login_at'`
	case "mysql":
		lookup = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'last_login_at'`
	default:
		lookup = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'last_login_at'`
	}
	if err := s.db.QueryRowContext(ctx, lookup).Scan(&count); err != nil {
		return fmt.Errorf("inspect users schema: %w", err)
	}
	if count > 0 {
		return nil
	}
	columnType := "TEXT"
	if s.driver == "mysql" {
		columnType = "VARCHAR(40)"
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE users ADD COLUMN last_login_at "+columnType); err != nil {
		return fmt.Errorf("add users last login column: %w", err)
	}
	return nil
}

func (s *SQLStore) ensureSystemSettingsGeoIPColumn(ctx context.Context) error {
	var count int
	var lookup string
	switch s.driver {
	case "sqlite":
		lookup = `SELECT COUNT(*) FROM pragma_table_info('system_settings') WHERE name = 'geoip_db_path'`
	case "mysql":
		lookup = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'system_settings' AND column_name = 'geoip_db_path'`
	default:
		lookup = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'system_settings' AND column_name = 'geoip_db_path'`
	}
	if err := s.db.QueryRowContext(ctx, lookup).Scan(&count); err != nil {
		return fmt.Errorf("inspect system settings schema: %w", err)
	}
	if count > 0 {
		return nil
	}
	columnType := "TEXT NOT NULL DEFAULT ''"
	if s.driver == "mysql" {
		columnType = "VARCHAR(4096) NOT NULL DEFAULT ''"
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE system_settings ADD COLUMN geoip_db_path "+columnType); err != nil {
		return fmt.Errorf("add system settings GeoIP path column: %w", err)
	}
	return nil
}

func (s *SQLStore) query(value string) string {
	if s.driver != "postgres" {
		return value
	}
	var out strings.Builder
	index := 1
	for _, char := range value {
		if char == '?' {
			fmt.Fprintf(&out, "$%d", index)
			index++
		} else {
			out.WriteRune(char)
		}
	}
	return out.String()
}

func (s *SQLStore) HasUsers() (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
func (s *SQLStore) CreateInitialAdmin(v User) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lockInsert := `INSERT INTO setup_locks (name) VALUES (?) ON CONFLICT (name) DO NOTHING`
	if s.driver == "mysql" {
		lockInsert = `INSERT IGNORE INTO setup_locks (name) VALUES (?)`
	}
	if _, err := tx.Exec(s.query(lockInsert), "initial_admin"); err != nil {
		return err
	}
	lockQuery := `SELECT name FROM setup_locks WHERE name = ?`
	if s.driver == "postgres" || s.driver == "mysql" {
		lockQuery += " FOR UPDATE"
	}
	var lockName string
	if err := tx.QueryRow(s.query(lockQuery), "initial_admin").Scan(&lockName); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errAlreadyInitialized
	}
	if _, err := tx.Exec(s.query(`INSERT INTO users (id, tenant_id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, strings.ToLower(v.Email), v.PasswordHash, v.Name, string(v.Role), timeText(v.CreatedAt)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLStore) GetUserByEmail(email string) (User, error) {
	return scanUser(s.db.QueryRow(s.query(`SELECT id, tenant_id, email, password_hash, name, role, last_login_at, created_at FROM users WHERE email = ?`), strings.ToLower(email)))
}
func (s *SQLStore) GetUser(id string) (User, error) {
	return scanUser(s.db.QueryRow(s.query(`SELECT id, tenant_id, email, password_hash, name, role, last_login_at, created_at FROM users WHERE id = ?`), id))
}
func (s *SQLStore) UpdateUserLastLogin(id string, at time.Time) error {
	return changed(s.db.Exec(s.query(`UPDATE users SET last_login_at = ? WHERE id = ?`), timeText(at), id))
}

func (s *SQLStore) CreateProject(v Project) error {
	_, err := s.db.Exec(s.query(`INSERT INTO projects (id, tenant_id, name, description, created_at) VALUES (?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.Name, v.Description, timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListProjects(tenant string) []Project {
	rows, err := s.db.Query(s.query(`SELECT id, tenant_id, name, description, created_at FROM projects WHERE tenant_id = ? ORDER BY created_at`), tenant)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		v, err := scanProject(rows)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}
func (s *SQLStore) GetProject(tenant, id string) (Project, error) {
	return scanProject(s.db.QueryRow(s.query(`SELECT id, tenant_id, name, description, created_at FROM projects WHERE tenant_id = ? AND id = ?`), tenant, id))
}

func (s *SQLStore) CreateNetwork(v Network) error {
	_, err := s.db.Exec(s.query(`INSERT INTO networks (id, tenant_id, project_id, name, cidr, dns, topology, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ProjectID, v.Name, v.CIDR, v.DNS, string(v.Topology), timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListNetworks(tenant, project string) []Network {
	rows, err := s.db.Query(s.query(`SELECT id, tenant_id, project_id, name, cidr, dns, topology, created_at FROM networks WHERE tenant_id = ? AND project_id = ? ORDER BY created_at`), tenant, project)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Network
	for rows.Next() {
		v, err := scanNetwork(rows)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}
func (s *SQLStore) GetNetwork(tenant, id string) (Network, error) {
	return scanNetwork(s.db.QueryRow(s.query(`SELECT id, tenant_id, project_id, name, cidr, dns, topology, created_at FROM networks WHERE tenant_id = ? AND id = ?`), tenant, id))
}

func (s *SQLStore) CreateNode(v Node) error {
	labels, _ := json.Marshal(v.Labels)
	secret, _ := json.Marshal(v.PrivateKey)
	wireGuard, _ := json.Marshal(v.WireGuard)
	_, err := s.db.Exec(s.query(`INSERT INTO nodes (id, tenant_id, project_id, network_id, name, address, endpoint, region, os, agent_version, labels_json, public_key, private_key_json, wireguard_json, last_seen, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ProjectID, v.NetworkID, v.Name, v.Address, v.Endpoint, v.Region, v.OS, v.AgentVersion, string(labels), v.PublicKey, string(secret), string(wireGuard), timeText(v.LastSeen), timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) GetNode(tenant, id string) (Node, error) {
	return scanNode(s.db.QueryRow(s.query(nodeSelect+` WHERE tenant_id = ? AND id = ?`), tenant, id))
}
func (s *SQLStore) GetNodeByID(id string) (Node, error) {
	return scanNode(s.db.QueryRow(s.query(nodeSelect+` WHERE id = ?`), id))
}
func (s *SQLStore) ListNodes(tenant, network string) []Node {
	query := nodeSelect + ` WHERE tenant_id = ?`
	args := []any{tenant}
	if network != "" {
		query += ` AND network_id = ?`
		args = append(args, network)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.Query(s.query(query), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		v, err := scanNode(rows)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}
func (s *SQLStore) UpdateNode(v Node) error {
	labels, _ := json.Marshal(v.Labels)
	secret, _ := json.Marshal(v.PrivateKey)
	wireGuard, _ := json.Marshal(v.WireGuard)
	result, err := s.db.Exec(s.query(`UPDATE nodes SET name=?, address=?, endpoint=?, region=?, os=?, agent_version=?, labels_json=?, public_key=?, private_key_json=?, wireguard_json=?, last_seen=? WHERE id=? AND tenant_id=?`), v.Name, v.Address, v.Endpoint, v.Region, v.OS, v.AgentVersion, string(labels), v.PublicKey, string(secret), string(wireGuard), timeText(v.LastSeen), v.ID, v.TenantID)
	return changed(result, err)
}

func (s *SQLStore) AddPeer(v PeerRelation) error {
	_, err := s.db.Exec(s.query(`INSERT INTO peer_relations (id, tenant_id, network_id, source_node_id, target_node_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.NetworkID, v.SourceNodeID, v.TargetNodeID, timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListPeers(tenant, network string) []PeerRelation {
	rows, err := s.db.Query(s.query(`SELECT id, tenant_id, network_id, source_node_id, target_node_id, created_at FROM peer_relations WHERE tenant_id=? AND network_id=? ORDER BY created_at`), tenant, network)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []PeerRelation
	for rows.Next() {
		var v PeerRelation
		var created string
		if rows.Scan(&v.ID, &v.TenantID, &v.NetworkID, &v.SourceNodeID, &v.TargetNodeID, &created) != nil {
			return nil
		}
		v.CreatedAt = parseTime(created)
		out = append(out, v)
	}
	return out
}

func (s *SQLStore) CreateRevision(v ConfigRevision) error {
	configs, err := json.Marshal(v.Configs)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(s.query(`INSERT INTO config_revisions (id, tenant_id, project_id, network_id, version, configs_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ProjectID, v.NetworkID, v.Version, string(configs), timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) LatestRevision(tenant, network string) (ConfigRevision, error) {
	var v ConfigRevision
	var configs, created string
	err := s.db.QueryRow(s.query(`SELECT id, tenant_id, project_id, network_id, version, configs_json, created_at FROM config_revisions WHERE tenant_id=? AND network_id=? ORDER BY version DESC LIMIT 1`), tenant, network).Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.NetworkID, &v.Version, &configs, &created)
	if err != nil {
		return ConfigRevision{}, notFound(err)
	}
	if err = json.Unmarshal([]byte(configs), &v.Configs); err != nil {
		return ConfigRevision{}, err
	}
	v.CreatedAt = parseTime(created)
	return v, nil
}

func (s *SQLStore) CreateDelivery(v ConfigDelivery) error {
	_, err := s.db.Exec(s.query(`INSERT INTO config_deliveries (id, tenant_id, node_id, version, state, message, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.NodeID, v.Version, v.State, v.Message, timeText(v.UpdatedAt))
	return err
}
func (s *SQLStore) UpdateDelivery(v ConfigDelivery) error {
	result, err := s.db.Exec(s.query(`UPDATE config_deliveries SET state=?, message=?, updated_at=? WHERE tenant_id=? AND node_id=? AND version=?`), v.State, v.Message, timeText(v.UpdatedAt), v.TenantID, v.NodeID, v.Version)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count > 0 {
		return nil
	}
	return s.CreateDelivery(v)
}
func (s *SQLStore) ListDeliveries(tenant, node string) []ConfigDelivery {
	query := `SELECT id, tenant_id, node_id, version, state, message, updated_at FROM config_deliveries WHERE tenant_id=?`
	args := []any{tenant}
	if node != "" {
		query += ` AND node_id=?`
		args = append(args, node)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.db.Query(s.query(query), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ConfigDelivery
	for rows.Next() {
		var v ConfigDelivery
		var updated string
		if rows.Scan(&v.ID, &v.TenantID, &v.NodeID, &v.Version, &v.State, &v.Message, &updated) != nil {
			return nil
		}
		v.UpdatedAt = parseTime(updated)
		out = append(out, v)
	}
	return out
}

func (s *SQLStore) CreateEnrollment(v EnrollmentToken) error {
	_, err := s.db.Exec(s.query(`INSERT INTO enrollment_tokens (id, tenant_id, project_id, network_id, token, expires_at, used_at) VALUES (?, ?, ?, ?, ?, ?, NULL)`), v.ID, v.TenantID, v.ProjectID, v.NetworkID, v.Token, timeText(v.ExpiresAt))
	return err
}
func (s *SQLStore) ConsumeEnrollment(token string) (EnrollmentToken, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return EnrollmentToken{}, err
	}
	defer tx.Rollback()
	var v EnrollmentToken
	var expires string
	var used sql.NullString
	err = tx.QueryRow(s.query(`SELECT id, tenant_id, project_id, network_id, token, expires_at, used_at FROM enrollment_tokens WHERE token=?`), token).Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.NetworkID, &v.Token, &expires, &used)
	if err != nil {
		return EnrollmentToken{}, notFound(err)
	}
	v.ExpiresAt = parseTime(expires)
	if used.Valid || time.Now().After(v.ExpiresAt) {
		return EnrollmentToken{}, errNotFound
	}
	now := time.Now()
	result, err := tx.Exec(s.query(`UPDATE enrollment_tokens SET used_at=? WHERE token=? AND used_at IS NULL`), timeText(now), token)
	if err != nil {
		return EnrollmentToken{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return EnrollmentToken{}, errNotFound
	}
	if err = tx.Commit(); err != nil {
		return EnrollmentToken{}, err
	}
	v.UsedAt = &now
	return v, nil
}

func (s *SQLStore) CreateIdentity(v AgentIdentity) error {
	_, err := s.db.Exec(s.query(`INSERT INTO agent_identities (node_id, certificate_pem, certificate_fingerprint, expires_at) VALUES (?, ?, ?, ?) ON CONFLICT (node_id) DO UPDATE SET certificate_pem=excluded.certificate_pem, certificate_fingerprint=excluded.certificate_fingerprint, expires_at=excluded.expires_at`), v.NodeID, v.CertificatePEM, v.CertificateFingerprint, timeText(v.ExpiresAt))
	return err
}
func (s *SQLStore) GetIdentity(node string) (AgentIdentity, error) {
	var v AgentIdentity
	var expires string
	err := s.db.QueryRow(s.query(`SELECT node_id, certificate_pem, certificate_fingerprint, expires_at FROM agent_identities WHERE node_id=?`), node).Scan(&v.NodeID, &v.CertificatePEM, &v.CertificateFingerprint, &expires)
	if err != nil {
		return AgentIdentity{}, notFound(err)
	}
	v.ExpiresAt = parseTime(expires)
	return v, nil
}

func (s *SQLStore) AddAudit(v AuditEvent) error {
	metadata, _ := json.Marshal(v.Metadata)
	_, err := s.db.Exec(s.query(`INSERT INTO audit_events (id, tenant_id, actor_id, action, resource_type, resource_id, metadata_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ActorID, v.Action, v.ResourceType, v.ResourceID, string(metadata), timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListAudit(tenant string) []AuditEvent {
	rows, err := s.db.Query(s.query(`SELECT id, tenant_id, actor_id, action, resource_type, resource_id, metadata_json, created_at FROM audit_events WHERE tenant_id=? ORDER BY created_at DESC`), tenant)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var v AuditEvent
		var metadata, created string
		if rows.Scan(&v.ID, &v.TenantID, &v.ActorID, &v.Action, &v.ResourceType, &v.ResourceID, &metadata, &created) != nil {
			return nil
		}
		_ = json.Unmarshal([]byte(metadata), &v.Metadata)
		v.CreatedAt = parseTime(created)
		out = append(out, v)
	}
	return out
}

func (s *SQLStore) ListUsers(tenant string) []User {
	rows, err := s.db.Query(s.query(`SELECT id, tenant_id, email, password_hash, name, role, last_login_at, created_at FROM users WHERE tenant_id = ? ORDER BY created_at`), tenant)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		v, err := scanUser(rows)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}
func (s *SQLStore) ensureNodeWireGuardColumn(ctx context.Context) error {
	var count int
	var lookup string
	switch s.driver {
	case "sqlite":
		lookup = `SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name = 'wireguard_json'`
	case "mysql":
		lookup = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'nodes' AND column_name = 'wireguard_json'`
	default:
		lookup = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'nodes' AND column_name = 'wireguard_json'`
	}
	if err := s.db.QueryRowContext(ctx, lookup).Scan(&count); err != nil {
		return fmt.Errorf("inspect nodes schema: %w", err)
	}
	if count > 0 {
		return nil
	}
	columnType := "TEXT NOT NULL DEFAULT '[]'"
	if s.driver == "mysql" {
		columnType = "LONGTEXT NULL"
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE nodes ADD COLUMN wireguard_json "+columnType); err != nil {
		return fmt.Errorf("add nodes wireguard column: %w", err)
	}
	if s.driver == "mysql" {
		if _, err := s.db.ExecContext(ctx, "UPDATE nodes SET wireguard_json = '[]' WHERE wireguard_json IS NULL"); err != nil {
			return fmt.Errorf("initialize nodes wireguard column: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) CreateUser(v User) error {
	_, err := s.db.Exec(s.query(`INSERT INTO users (id, tenant_id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, strings.ToLower(v.Email), v.PasswordHash, v.Name, string(v.Role), timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) GetSettings(tenant string) (SystemSettings, error) {
	var raw, geoPath, updated string
	if err := s.db.QueryRow(s.query(`SELECT settings_json, geoip_db_path, updated_at FROM system_settings WHERE tenant_id = ?`), tenant).Scan(&raw, &geoPath, &updated); err != nil {
		return SystemSettings{}, notFound(err)
	}
	var v SystemSettings
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return SystemSettings{}, err
	}
	v.TenantID, v.GeoIPDBPath, v.UpdatedAt = tenant, geoPath, parseTime(updated)
	return v, nil
}
func (s *SQLStore) UpsertSettings(v SystemSettings) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	query := `INSERT INTO system_settings (tenant_id, settings_json, geoip_db_path, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT (tenant_id) DO UPDATE SET settings_json=excluded.settings_json, geoip_db_path=excluded.geoip_db_path, updated_at=excluded.updated_at`
	if s.driver == "mysql" {
		query = `INSERT INTO system_settings (tenant_id, settings_json, geoip_db_path, updated_at) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE settings_json=VALUES(settings_json), geoip_db_path=VALUES(geoip_db_path), updated_at=VALUES(updated_at)`
	}
	_, err = s.db.Exec(s.query(query), v.TenantID, string(raw), v.GeoIPDBPath, timeText(v.UpdatedAt))
	return err
}
func (s *SQLStore) ListNotificationChannels(tenant string) []NotificationChannel {
	rows, err := s.db.Query(s.query(`SELECT id, tenant_id, name, type, target_json, enabled, all_agents, agent_ids_json, created_at, updated_at FROM notification_channels WHERE tenant_id = ? ORDER BY created_at`), tenant)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []NotificationChannel
	for rows.Next() {
		v, err := scanNotificationChannel(rows)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}
func (s *SQLStore) GetNotificationChannel(tenant, id string) (NotificationChannel, error) {
	return scanNotificationChannel(s.db.QueryRow(s.query(`SELECT id, tenant_id, name, type, target_json, enabled, all_agents, agent_ids_json, created_at, updated_at FROM notification_channels WHERE tenant_id = ? AND id = ?`), tenant, id))
}
func (s *SQLStore) CreateNotificationChannel(v NotificationChannel) error {
	target, _ := json.Marshal(v.Target)
	agents, _ := json.Marshal(v.AgentIDs)
	_, err := s.db.Exec(s.query(`INSERT INTO notification_channels (id, tenant_id, name, type, target_json, enabled, all_agents, agent_ids_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.Name, v.Type, string(target), v.Enabled, v.AllAgents, string(agents), timeText(v.CreatedAt), timeText(v.UpdatedAt))
	return err
}
func (s *SQLStore) UpdateNotificationChannel(v NotificationChannel) error {
	target, _ := json.Marshal(v.Target)
	agents, _ := json.Marshal(v.AgentIDs)
	return changed(s.db.Exec(s.query(`UPDATE notification_channels SET name=?, type=?, target_json=?, enabled=?, all_agents=?, agent_ids_json=?, updated_at=? WHERE tenant_id=? AND id=?`), v.Name, v.Type, string(target), v.Enabled, v.AllAgents, string(agents), timeText(v.UpdatedAt), v.TenantID, v.ID))
}
func (s *SQLStore) DeleteNotificationChannel(tenant, id string) error {
	return changed(s.db.Exec(s.query(`DELETE FROM notification_channels WHERE tenant_id=? AND id=?`), tenant, id))
}
func (s *SQLStore) AddNotificationLog(v NotificationLog) error {
	_, err := s.db.Exec(s.query(`INSERT INTO notification_logs (id, tenant_id, channel_id, channel_name, channel_type, agent_name, message, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ChannelID, v.ChannelName, v.ChannelType, v.AgentName, v.Message, v.Status, timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListNotificationLogs(tenant string) []NotificationLog {
	rows, err := s.db.Query(s.query(`SELECT id, tenant_id, channel_id, channel_name, channel_type, agent_name, message, status, created_at FROM notification_logs WHERE tenant_id=? ORDER BY created_at DESC`), tenant)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []NotificationLog
	for rows.Next() {
		var v NotificationLog
		var created string
		if rows.Scan(&v.ID, &v.TenantID, &v.ChannelID, &v.ChannelName, &v.ChannelType, &v.AgentName, &v.Message, &v.Status, &created) != nil {
			return nil
		}
		v.CreatedAt = parseTime(created)
		out = append(out, v)
	}
	return out
}
func scanNotificationChannel(row scanner) (NotificationChannel, error) {
	var v NotificationChannel
	var target, agents, created, updated string
	if err := row.Scan(&v.ID, &v.TenantID, &v.Name, &v.Type, &target, &v.Enabled, &v.AllAgents, &agents, &created, &updated); err != nil {
		return NotificationChannel{}, notFound(err)
	}
	if err := json.Unmarshal([]byte(target), &v.Target); err != nil {
		return NotificationChannel{}, err
	}
	if err := json.Unmarshal([]byte(agents), &v.AgentIDs); err != nil {
		return NotificationChannel{}, err
	}
	v.CreatedAt, v.UpdatedAt = parseTime(created), parseTime(updated)
	return v, nil
}

const nodeSelect = `SELECT id, tenant_id, project_id, network_id, name, address, endpoint, region, os, agent_version, labels_json, public_key, private_key_json, wireguard_json, last_seen, created_at FROM nodes`

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (User, error) {
	var v User
	var role, created string
	var lastLogin sql.NullString
	if err := row.Scan(&v.ID, &v.TenantID, &v.Email, &v.PasswordHash, &v.Name, &role, &lastLogin, &created); err != nil {
		return User{}, notFound(err)
	}
	v.Role = Role(role)
	if lastLogin.Valid {
		v.LastLoginAt = parseTime(lastLogin.String)
	}
	v.CreatedAt = parseTime(created)
	return v, nil
}
func scanProject(row scanner) (Project, error) {
	var v Project
	var created string
	if err := row.Scan(&v.ID, &v.TenantID, &v.Name, &v.Description, &created); err != nil {
		return Project{}, notFound(err)
	}
	v.CreatedAt = parseTime(created)
	return v, nil
}
func scanNetwork(row scanner) (Network, error) {
	var v Network
	var topology, created string
	if err := row.Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.Name, &v.CIDR, &v.DNS, &topology, &created); err != nil {
		return Network{}, notFound(err)
	}
	v.Topology = Topology(topology)
	v.CreatedAt = parseTime(created)
	return v, nil
}
func scanNode(row scanner) (Node, error) {
	var v Node
	var labels, secret, lastSeen, created string
	var wireGuard sql.NullString
	if err := row.Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.NetworkID, &v.Name, &v.Address, &v.Endpoint, &v.Region, &v.OS, &v.AgentVersion, &labels, &v.PublicKey, &secret, &wireGuard, &lastSeen, &created); err != nil {
		return Node{}, notFound(err)
	}
	if err := json.Unmarshal([]byte(labels), &v.Labels); err != nil {
		return Node{}, err
	}
	if err := json.Unmarshal([]byte(secret), &v.PrivateKey); err != nil {
		return Node{}, err
	}
	if wireGuard.Valid && strings.TrimSpace(wireGuard.String) != "" {
		if err := json.Unmarshal([]byte(wireGuard.String), &v.WireGuard); err != nil {
			return Node{}, err
		}
	}
	if v.WireGuard == nil {
		v.WireGuard = []WireGuardInterfaceStatus{}
	}
	v.LastSeen = parseTime(lastSeen)
	v.CreatedAt = parseTime(created)
	return v, nil
}

const databaseTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func timeText(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(databaseTimeLayout)
}
func parseTime(value string) time.Time {
	parsed, err := time.Parse(databaseTimeLayout, value)
	if err != nil {
		parsed, _ = time.Parse(time.RFC3339Nano, value)
	}
	return parsed
}
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	return err
}
func changed(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errNotFound
	}
	return nil
}
