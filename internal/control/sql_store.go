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
	return buildSchemaStatements("sqlite")
}

func mysqlSchemaStatements() []string {
	return buildSchemaStatements("mysql")
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
	if err := s.ensureNodeWireGuardColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureNodeAgentStatusColumns(ctx); err != nil {
		return err
	}
	return s.ensureNodeConfigColumns(ctx)
}

func (s *SQLStore) ensureUserLastLoginColumn(ctx context.Context) error {
	return s.ensureSchemaColumn(
		ctx,
		"users",
		schemaColumn{name: "last_login_at", definition: "TEXT", mysqlDefinition: "VARCHAR(40)"},
		"inspect users schema",
		"add users last login column",
	)
}

func (s *SQLStore) ensureSystemSettingsGeoIPColumn(ctx context.Context) error {
	return s.ensureSchemaColumn(
		ctx,
		"system_settings",
		schemaColumn{name: "geoip_db_path", definition: "TEXT NOT NULL DEFAULT ''", mysqlDefinition: "VARCHAR(4096) NOT NULL DEFAULT ''"},
		"inspect system settings schema",
		"add system settings GeoIP path column",
	)
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

func queryList[T any](store *SQLStore, query string, scan func(scanner) (T, error), args ...any) ([]T, error) {
	rows, err := store.db.Query(store.query(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]T, 0)
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// marshalColumn encodes a JSON column value. The input types are JSON-safe, but
// returning the error keeps future struct changes from silently persisting an
// empty column when a field stops being serializable.
func marshalColumn(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func marshalNodeColumns(v Node) (labels, secret, wireGuard, peerConfig, desiredPeerConfig string, err error) {
	if labels, err = marshalColumn(v.Labels); err != nil {
		return
	}
	if secret, err = marshalColumn(v.PrivateKey); err != nil {
		return
	}
	if wireGuard, err = marshalColumn(v.WireGuard); err != nil {
		return
	}
	if peerConfig, err = marshalColumn(v.PeerConfigFiles); err != nil {
		return
	}
	desiredPeerConfig, err = marshalColumn(v.DesiredPeerConfig)
	return
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

func (s *SQLStore) ensureNodeConfigColumns(ctx context.Context) error {
	columns := []schemaColumn{
		{name: "enabled", definition: "BOOLEAN NOT NULL DEFAULT TRUE"},
		{name: "listen_port", definition: "INTEGER NOT NULL DEFAULT 51820"},
		{name: "mtu", definition: "INTEGER NOT NULL DEFAULT 1420"},
		{name: "location_name", definition: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "location_source", definition: "VARCHAR(32) NOT NULL DEFAULT ''"},
		{name: "latitude", definition: "DOUBLE PRECISION NOT NULL DEFAULT 0"},
		{name: "longitude", definition: "DOUBLE PRECISION NOT NULL DEFAULT 0"},
		{name: "peer_config_json", definition: "TEXT NOT NULL DEFAULT '[]'", mysqlDefinition: "LONGTEXT NULL"},
		{name: "desired_peer_config_json", definition: "TEXT NOT NULL DEFAULT '[]'", mysqlDefinition: "LONGTEXT NULL"},
	}
	return s.ensureNodeSchemaColumns(ctx, columns)
}

func (s *SQLStore) CreateProject(v Project) error {
	_, err := s.db.Exec(s.query(`INSERT INTO projects (id, tenant_id, name, description, created_at) VALUES (?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.Name, v.Description, timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListProjects(tenant string) ([]Project, error) {
	return queryList(s, `SELECT id, tenant_id, name, description, created_at FROM projects WHERE tenant_id = ? ORDER BY created_at`, scanProject, tenant)
}
func (s *SQLStore) GetProject(tenant, id string) (Project, error) {
	return scanProject(s.db.QueryRow(s.query(`SELECT id, tenant_id, name, description, created_at FROM projects WHERE tenant_id = ? AND id = ?`), tenant, id))
}

func (s *SQLStore) CreateNetwork(v Network) error {
	_, err := s.db.Exec(s.query(`INSERT INTO networks (id, tenant_id, project_id, name, cidr, dns, topology, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ProjectID, v.Name, v.CIDR, v.DNS, string(v.Topology), timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListNetworks(tenant, project string) ([]Network, error) {
	query := `SELECT id, tenant_id, project_id, name, cidr, dns, topology, created_at FROM networks WHERE tenant_id = ?`
	args := []any{tenant}
	if project != "" {
		query += ` AND project_id = ?`
		args = append(args, project)
	}
	query += ` ORDER BY created_at`
	return queryList(s, query, scanNetwork, args...)
}
func (s *SQLStore) GetNetwork(tenant, id string) (Network, error) {
	return scanNetwork(s.db.QueryRow(s.query(`SELECT id, tenant_id, project_id, name, cidr, dns, topology, created_at FROM networks WHERE tenant_id = ? AND id = ?`), tenant, id))
}

func (s *SQLStore) CreateNode(v Node) error {
	v = normalizeNodeDefaults(v)
	labels, secret, wireGuard, peerConfig, desiredPeerConfig, err := marshalNodeColumns(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(s.query(`INSERT INTO nodes (id, tenant_id, project_id, network_id, name, hostname, interface_selector, collection_error, enabled, listen_port, mtu, address, endpoint, region, location_name, location_source, latitude, longitude, os, agent_version, labels_json, public_key, private_key_json, wireguard_json, peer_config_json, desired_peer_config_json, last_seen, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ProjectID, v.NetworkID, v.Name, v.Hostname, v.InterfaceSelector, v.CollectionError, v.Enabled, v.ListenPort, v.MTU, v.Address, v.Endpoint, v.Region, v.LocationName, v.LocationSource, v.Latitude, v.Longitude, v.OS, v.AgentVersion, labels, v.PublicKey, secret, wireGuard, peerConfig, desiredPeerConfig, timeText(v.LastSeen), timeText(v.CreatedAt))
	if err != nil && isAddressConflictError(err) {
		return errAddressConflict
	}
	return err
}

// isAddressConflictError 识别三驱动对 nodes(network_id, address) 唯一约束冲突
// 的错误，用于把并发 enroll 的地址竞争转换为可重试的哨兵错误。
func isAddressConflictError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "UNIQUE constraint failed: nodes.address") ||
		strings.Contains(text, "Duplicate entry") ||
		strings.Contains(text, "duplicate key value violates unique constraint")
}
func (s *SQLStore) GetNode(tenant, id string) (Node, error) {
	return scanNode(s.db.QueryRow(s.query(nodeSelect+` WHERE tenant_id = ? AND id = ?`), tenant, id))
}
func (s *SQLStore) GetNodeByID(id string) (Node, error) {
	return scanNode(s.db.QueryRow(s.query(nodeSelect+` WHERE id = ?`), id))
}
func (s *SQLStore) ListNodes(tenant, network string) ([]Node, error) {
	query := nodeSelect + ` WHERE tenant_id = ?`
	args := []any{tenant}
	if network != "" {
		query += ` AND network_id = ?`
		args = append(args, network)
	}
	query += ` ORDER BY created_at`
	return queryList(s, query, scanNode, args...)
}
func (s *SQLStore) ListNodeRefs(tenant, network string) ([]Node, error) {
	query := nodeRefSelect + ` WHERE tenant_id = ?`
	args := []any{tenant}
	if network != "" {
		query += ` AND network_id = ?`
		args = append(args, network)
	}
	query += ` ORDER BY created_at`
	return queryList(s, query, scanNodeRef, args...)
}
func (s *SQLStore) UpdateNode(v Node) error {
	labels, secret, wireGuard, peerConfig, desiredPeerConfig, err := marshalNodeColumns(v)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(s.query(`UPDATE nodes SET name=?, hostname=?, interface_selector=?, collection_error=?, enabled=?, listen_port=?, mtu=?, address=?, endpoint=?, region=?, location_name=?, location_source=?, latitude=?, longitude=?, os=?, agent_version=?, labels_json=?, public_key=?, private_key_json=?, wireguard_json=?, peer_config_json=?, desired_peer_config_json=?, last_seen=? WHERE id=? AND tenant_id=?`), v.Name, v.Hostname, v.InterfaceSelector, v.CollectionError, v.Enabled, v.ListenPort, v.MTU, v.Address, v.Endpoint, v.Region, v.LocationName, v.LocationSource, v.Latitude, v.Longitude, v.OS, v.AgentVersion, labels, v.PublicKey, secret, wireGuard, peerConfig, desiredPeerConfig, timeText(v.LastSeen), v.ID, v.TenantID)
	return changed(result, err)
}

func (s *SQLStore) DeleteNode(tenant, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DELETE FROM peer_relations WHERE tenant_id=? AND (source_node_id=? OR target_node_id=?)`,
		`DELETE FROM config_deliveries WHERE tenant_id=? AND node_id=?`,
		`DELETE FROM agent_commands WHERE tenant_id=? AND node_id=?`,
		`DELETE FROM traffic_samples WHERE tenant_id=? AND node_id=?`,
	} {
		args := []any{tenant, id}
		if strings.Contains(statement, "source_node_id") {
			args = []any{tenant, id, id}
		}
		if _, err := tx.Exec(s.query(statement), args...); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(s.query(`DELETE FROM agent_identities WHERE node_id=?`), id); err != nil {
		return err
	}
	result, err := tx.Exec(s.query(`DELETE FROM nodes WHERE tenant_id=? AND id=?`), tenant, id)
	if err := changed(result, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) AddTrafficSamples(samples []TrafficSample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(s.query(`INSERT INTO traffic_samples (id, tenant_id, node_id, interface_name, receive_bytes, transmit_bytes, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?)`))
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, v := range samples {
		if _, err := stmt.Exec(v.ID, v.TenantID, v.NodeID, v.InterfaceName, v.ReceiveBytes, v.TransmitBytes, timeText(v.RecordedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *SQLStore) ListTrafficSamples(tenant, node, iface string, since time.Time) ([]TrafficSample, error) {
	return queryList(s, `SELECT id, tenant_id, node_id, interface_name, receive_bytes, transmit_bytes, recorded_at FROM traffic_samples WHERE tenant_id=? AND node_id=? AND interface_name=? AND recorded_at>=? ORDER BY recorded_at`, scanTrafficSample, tenant, node, iface, timeText(since))
}

func (s *SQLStore) AddPeer(v PeerRelation) error {
	_, err := s.db.Exec(s.query(`INSERT INTO peer_relations (id, tenant_id, network_id, source_node_id, target_node_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.NetworkID, v.SourceNodeID, v.TargetNodeID, timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListPeers(tenant, network string) ([]PeerRelation, error) {
	return queryList(s, `SELECT id, tenant_id, network_id, source_node_id, target_node_id, created_at FROM peer_relations WHERE tenant_id=? AND network_id=? ORDER BY created_at`, scanPeerRelation, tenant, network)
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
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return s.CreateDelivery(v)
}
func (s *SQLStore) ListDeliveries(tenant, node string) ([]ConfigDelivery, error) {
	query := `SELECT id, tenant_id, node_id, version, state, message, updated_at FROM config_deliveries WHERE tenant_id=?`
	args := []any{tenant}
	if node != "" {
		query += ` AND node_id=?`
		args = append(args, node)
	}
	query += ` ORDER BY updated_at DESC`
	return queryList(s, query, scanDelivery, args...)
}

func (s *SQLStore) CreateCommand(v AgentCommand) error {
	_, err := s.db.Exec(s.query(`INSERT INTO agent_commands (id, tenant_id, node_id, type, state, result, created_at, started_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.NodeID, v.Type, v.State, v.Result, timeText(v.CreatedAt), optionalTimeText(v.StartedAt), optionalTimeText(v.CompletedAt))
	if err != nil {
		return err
	}
	return s.pruneCommands(v.TenantID, v.NodeID)
}
func (s *SQLStore) ClaimCommands(node string) []AgentCommand {
	tx, err := s.db.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	claimQuery := `SELECT id, tenant_id, node_id, type, state, result, created_at, started_at, completed_at FROM agent_commands WHERE node_id=? AND state='pending' ORDER BY created_at`
	// Lock the selected rows so concurrent control-plane instances cannot claim
	// the same pending command. SQLite is a single-connection store and is
	// already serialized by MaxOpenConns(1).
	switch s.driver {
	case "postgres":
		claimQuery += ` FOR UPDATE SKIP LOCKED`
	case "mysql":
		claimQuery += ` FOR UPDATE`
	}
	rows, err := tx.Query(s.query(claimQuery), node)
	if err != nil {
		return nil
	}
	var out []AgentCommand
	for rows.Next() {
		command, err := scanCommand(rows)
		if err != nil {
			rows.Close()
			return nil
		}
		out = append(out, command)
	}
	rows.Close()
	now := time.Now()
	for index := range out {
		out[index].State = "running"
		out[index].StartedAt = &now
		if _, err := tx.Exec(s.query(`UPDATE agent_commands SET state=?, started_at=? WHERE id=? AND state='pending'`), out[index].State, timeText(now), out[index].ID); err != nil {
			return nil
		}
	}
	if err := tx.Commit(); err != nil {
		return nil
	}
	return out
}
func (s *SQLStore) UpdateCommand(v AgentCommand) error {
	result, err := s.db.Exec(s.query(`UPDATE agent_commands SET state=?, result=?, started_at=?, completed_at=? WHERE id=? AND tenant_id=? AND node_id=?`), v.State, v.Result, optionalTimeText(v.StartedAt), optionalTimeText(v.CompletedAt), v.ID, v.TenantID, v.NodeID)
	return changed(result, err)
}
func (s *SQLStore) GetCommand(id string) (AgentCommand, error) {
	return scanCommand(s.db.QueryRow(s.query(`SELECT id, tenant_id, node_id, type, state, result, created_at, started_at, completed_at FROM agent_commands WHERE id=?`), id))
}
func (s *SQLStore) ListCommands(tenant, node string) ([]AgentCommand, error) {
	query := `SELECT id, tenant_id, node_id, type, state, result, created_at, started_at, completed_at FROM agent_commands WHERE tenant_id=?`
	args := []any{tenant}
	if node != "" {
		query += ` AND node_id=?`
		args = append(args, node)
	}
	query += ` ORDER BY created_at DESC`
	return queryList(s, query, scanCommand, args...)
}

func (s *SQLStore) ListCommandsPage(tenant, node string, limit, offset int, errorsOnly bool) ([]AgentCommand, error) {
	query := `SELECT id, tenant_id, node_id, type, state, result, created_at, started_at, completed_at FROM agent_commands WHERE tenant_id=?`
	args := []any{tenant}
	if node != "" {
		query += ` AND node_id=?`
		args = append(args, node)
	}
	if errorsOnly {
		query += ` AND state='failed'`
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	return queryList(s, query, scanCommand, args...)
}

func (s *SQLStore) ClearCommands(tenant, node string) error {
	_, err := s.db.Exec(s.query(`DELETE FROM agent_commands WHERE tenant_id=? AND node_id=?`), tenant, node)
	return err
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
	count, err := result.RowsAffected()
	if err != nil {
		return EnrollmentToken{}, err
	}
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
	metadata, err := marshalColumn(v.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(s.query(`INSERT INTO audit_events (id, tenant_id, actor_id, action, resource_type, resource_id, metadata_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ActorID, v.Action, v.ResourceType, v.ResourceID, metadata, timeText(v.CreatedAt))
	if err != nil {
		return err
	}
	return s.pruneAudit(v.TenantID)
}
func (s *SQLStore) ListAudit(tenant string) ([]AuditEvent, error) {
	return queryList(s, `SELECT id, tenant_id, actor_id, action, resource_type, resource_id, metadata_json, created_at FROM audit_events WHERE tenant_id=? ORDER BY created_at DESC`, scanAudit, tenant)
}

func (s *SQLStore) ListAuditPage(tenant string, limit, offset int) ([]AuditEvent, error) {
	return queryList(s, `SELECT id, tenant_id, actor_id, action, resource_type, resource_id, metadata_json, created_at FROM audit_events WHERE tenant_id=? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, scanAudit, tenant, limit, offset)
}

func (s *SQLStore) HasNodeAuditAction(tenant, nodeID string, actions ...string) (bool, error) {
	if len(actions) == 0 {
		return false, nil
	}
	placeholders := make([]string, len(actions))
	args := make([]any, 0, len(actions)+2)
	args = append(args, tenant, nodeID)
	for i, action := range actions {
		placeholders[i] = "?"
		args = append(args, action)
	}
	query := `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND resource_type='node' AND resource_id=? AND action IN (` + strings.Join(placeholders, ",") + `)`
	var count int
	if err := s.db.QueryRow(s.query(query), args...).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SQLStore) ClearAudit(tenant string) error {
	_, err := s.db.Exec(s.query(`DELETE FROM audit_events WHERE tenant_id=?`), tenant)
	return err
}

func (s *SQLStore) pruneCommands(tenant, node string) error {
	return s.pruneKeepingNewest("agent_commands", tenant, node, maxAgentLogRecords)
}

func (s *SQLStore) pruneAudit(tenant string) error {
	return s.pruneKeepingNewest("audit_events", tenant, "", maxAuditRecords)
}

// pruneKeepingNewest keeps at most `keep` newest rows for the given tenant (and
// optional node) scope. It first reads the (created_at, id) boundary of the
// keep-th newest row, so the common case (fewer than keep rows) is a single
// bounded lookup instead of a full count plus a subquery delete. When pruning
// is required the range delete uses the existing tenant/index columns directly.
func (s *SQLStore) pruneKeepingNewest(table, tenant, node string, keep int) error {
	where := `tenant_id=?`
	args := []any{tenant}
	if node != "" {
		where += ` AND node_id=?`
		args = append(args, node)
	}
	boundaryArgs := append(append([]any{}, args...), keep-1)
	boundaryQuery := fmt.Sprintf(`SELECT created_at, id FROM %s WHERE %s ORDER BY created_at DESC, id DESC LIMIT 1 OFFSET ?`, table, where)
	var boundaryCreated, boundaryID string
	err := s.db.QueryRow(s.query(boundaryQuery), boundaryArgs...).Scan(&boundaryCreated, &boundaryID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	deleteArgs := append(append([]any{}, args...), boundaryCreated, boundaryCreated, boundaryID)
	deleteQuery := fmt.Sprintf(`DELETE FROM %s WHERE %s AND (created_at < ? OR (created_at = ? AND id < ?))`, table, where)
	_, err = s.db.Exec(s.query(deleteQuery), deleteArgs...)
	return err
}

func (s *SQLStore) ListUsers(tenant string) ([]User, error) {
	return queryList(s, `SELECT id, tenant_id, email, password_hash, name, role, last_login_at, created_at FROM users WHERE tenant_id = ? ORDER BY created_at`, scanUser, tenant)
}
func (s *SQLStore) ensureNodeWireGuardColumn(ctx context.Context) error {
	return s.ensureSchemaColumn(
		ctx,
		"nodes",
		schemaColumn{
			name:                    "wireguard_json",
			definition:              "TEXT NOT NULL DEFAULT '[]'",
			mysqlDefinition:         "LONGTEXT NULL",
			mysqlInitializeNullSQL:  "UPDATE nodes SET wireguard_json = '[]' WHERE wireguard_json IS NULL",
			mysqlInitializeNullDesc: "initialize nodes wireguard column",
		},
		"inspect nodes schema",
		"add nodes wireguard column",
	)
}

func (s *SQLStore) ensureNodeAgentStatusColumns(ctx context.Context) error {
	columns := []schemaColumn{
		{name: "hostname", definition: "TEXT NOT NULL DEFAULT ''", mysqlDefinition: "VARCHAR(255) NULL"},
		{name: "interface_selector", definition: "TEXT NOT NULL DEFAULT ''", mysqlDefinition: "VARCHAR(255) NULL"},
		{name: "collection_error", definition: "TEXT NOT NULL DEFAULT ''", mysqlDefinition: "LONGTEXT NULL"},
	}
	return s.ensureNodeSchemaColumns(ctx, columns)
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
func (s *SQLStore) ListNotificationChannels(tenant string) ([]NotificationChannel, error) {
	return queryList(s, `SELECT id, tenant_id, name, type, target_json, enabled, all_agents, agent_ids_json, created_at, updated_at FROM notification_channels WHERE tenant_id = ? ORDER BY created_at`, scanNotificationChannel, tenant)
}
func (s *SQLStore) GetNotificationChannel(tenant, id string) (NotificationChannel, error) {
	return scanNotificationChannel(s.db.QueryRow(s.query(`SELECT id, tenant_id, name, type, target_json, enabled, all_agents, agent_ids_json, created_at, updated_at FROM notification_channels WHERE tenant_id = ? AND id = ?`), tenant, id))
}
func (s *SQLStore) CreateNotificationChannel(v NotificationChannel) error {
	target, err := marshalColumn(v.Target)
	if err != nil {
		return err
	}
	agents, err := marshalColumn(v.AgentIDs)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(s.query(`INSERT INTO notification_channels (id, tenant_id, name, type, target_json, enabled, all_agents, agent_ids_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.Name, v.Type, target, v.Enabled, v.AllAgents, agents, timeText(v.CreatedAt), timeText(v.UpdatedAt))
	return err
}
func (s *SQLStore) UpdateNotificationChannel(v NotificationChannel) error {
	target, err := marshalColumn(v.Target)
	if err != nil {
		return err
	}
	agents, err := marshalColumn(v.AgentIDs)
	if err != nil {
		return err
	}
	return changed(s.db.Exec(s.query(`UPDATE notification_channels SET name=?, type=?, target_json=?, enabled=?, all_agents=?, agent_ids_json=?, updated_at=? WHERE tenant_id=? AND id=?`), v.Name, v.Type, target, v.Enabled, v.AllAgents, agents, timeText(v.UpdatedAt), v.TenantID, v.ID))
}
func (s *SQLStore) DeleteNotificationChannel(tenant, id string) error {
	return changed(s.db.Exec(s.query(`DELETE FROM notification_channels WHERE tenant_id=? AND id=?`), tenant, id))
}
func (s *SQLStore) AddNotificationLog(v NotificationLog) error {
	_, err := s.db.Exec(s.query(`INSERT INTO notification_logs (id, tenant_id, channel_id, channel_name, channel_type, agent_name, message, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.TenantID, v.ChannelID, v.ChannelName, v.ChannelType, v.AgentName, v.Message, v.Status, timeText(v.CreatedAt))
	return err
}
func (s *SQLStore) ListNotificationLogs(tenant string) ([]NotificationLog, error) {
	return queryList(s, `SELECT id, tenant_id, channel_id, channel_name, channel_type, agent_name, message, status, created_at FROM notification_logs WHERE tenant_id=? ORDER BY created_at DESC`, scanNotificationLog, tenant)
}

func scanTrafficSample(row scanner) (TrafficSample, error) {
	var v TrafficSample
	var at string
	if err := row.Scan(&v.ID, &v.TenantID, &v.NodeID, &v.InterfaceName, &v.ReceiveBytes, &v.TransmitBytes, &at); err != nil {
		return TrafficSample{}, err
	}
	v.RecordedAt = parseTime(at)
	return v, nil
}

func scanPeerRelation(row scanner) (PeerRelation, error) {
	var v PeerRelation
	var created string
	if err := row.Scan(&v.ID, &v.TenantID, &v.NetworkID, &v.SourceNodeID, &v.TargetNodeID, &created); err != nil {
		return PeerRelation{}, err
	}
	v.CreatedAt = parseTime(created)
	return v, nil
}

func scanDelivery(row scanner) (ConfigDelivery, error) {
	var v ConfigDelivery
	var updated string
	if err := row.Scan(&v.ID, &v.TenantID, &v.NodeID, &v.Version, &v.State, &v.Message, &updated); err != nil {
		return ConfigDelivery{}, err
	}
	v.UpdatedAt = parseTime(updated)
	return v, nil
}

func scanAudit(row scanner) (AuditEvent, error) {
	var v AuditEvent
	var metadata, created string
	if err := row.Scan(&v.ID, &v.TenantID, &v.ActorID, &v.Action, &v.ResourceType, &v.ResourceID, &metadata, &created); err != nil {
		return AuditEvent{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &v.Metadata)
	v.CreatedAt = parseTime(created)
	return v, nil
}

func scanNotificationLog(row scanner) (NotificationLog, error) {
	var v NotificationLog
	var created string
	if err := row.Scan(&v.ID, &v.TenantID, &v.ChannelID, &v.ChannelName, &v.ChannelType, &v.AgentName, &v.Message, &v.Status, &created); err != nil {
		return NotificationLog{}, err
	}
	v.CreatedAt = parseTime(created)
	return v, nil
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

const nodeSelect = `SELECT id, tenant_id, project_id, network_id, name, COALESCE(hostname, ''), COALESCE(interface_selector, ''), COALESCE(collection_error, ''), enabled, listen_port, mtu, address, endpoint, region, COALESCE(location_name, ''), COALESCE(location_source, ''), COALESCE(latitude, 0), COALESCE(longitude, 0), os, agent_version, labels_json, public_key, private_key_json, wireguard_json, COALESCE(peer_config_json, '[]'), COALESCE(desired_peer_config_json, '[]'), last_seen, created_at FROM nodes`

// nodeRefSelect omits the large per-node JSON columns (private key, WireGuard
// status, and peer config files) for callers that only need scalar identity and
// routing fields, avoiding the cost of decoding those blobs on every heartbeat.
const nodeRefSelect = `SELECT id, tenant_id, project_id, network_id, name, COALESCE(hostname, ''), COALESCE(interface_selector, ''), COALESCE(collection_error, ''), enabled, listen_port, mtu, address, endpoint, region, COALESCE(location_name, ''), COALESCE(location_source, ''), COALESCE(latitude, 0), COALESCE(longitude, 0), os, agent_version, labels_json, public_key, last_seen, created_at FROM nodes`

func scanCommand(row scanner) (AgentCommand, error) {
	var v AgentCommand
	var created string
	var started, completed sql.NullString
	if err := row.Scan(&v.ID, &v.TenantID, &v.NodeID, &v.Type, &v.State, &v.Result, &created, &started, &completed); err != nil {
		return AgentCommand{}, notFound(err)
	}
	v.CreatedAt = parseTime(created)
	if started.Valid {
		value := parseTime(started.String)
		v.StartedAt = &value
	}
	if completed.Valid {
		value := parseTime(completed.String)
		v.CompletedAt = &value
	}
	return v, nil
}
func optionalTimeText(value *time.Time) any {
	if value == nil {
		return nil
	}
	return timeText(*value)
}

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
	var peerConfig, desiredPeerConfig sql.NullString
	if err := row.Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.NetworkID, &v.Name, &v.Hostname, &v.InterfaceSelector, &v.CollectionError, &v.Enabled, &v.ListenPort, &v.MTU, &v.Address, &v.Endpoint, &v.Region, &v.LocationName, &v.LocationSource, &v.Latitude, &v.Longitude, &v.OS, &v.AgentVersion, &labels, &v.PublicKey, &secret, &wireGuard, &peerConfig, &desiredPeerConfig, &lastSeen, &created); err != nil {
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
	if peerConfig.Valid && strings.TrimSpace(peerConfig.String) != "" {
		if err := json.Unmarshal([]byte(peerConfig.String), &v.PeerConfigFiles); err != nil {
			return Node{}, err
		}
	}
	if desiredPeerConfig.Valid && strings.TrimSpace(desiredPeerConfig.String) != "" {
		if err := json.Unmarshal([]byte(desiredPeerConfig.String), &v.DesiredPeerConfig); err != nil {
			return Node{}, err
		}
	}
	if v.PeerConfigFiles == nil {
		v.PeerConfigFiles = []PeerConfigFile{}
	}
	if v.DesiredPeerConfig == nil {
		v.DesiredPeerConfig = []PeerConfigFile{}
	}
	v.LastSeen = parseTime(lastSeen)
	v.CreatedAt = parseTime(created)
	return v, nil
}

func scanNodeRef(row scanner) (Node, error) {
	var v Node
	var labels, lastSeen, created string
	if err := row.Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.NetworkID, &v.Name, &v.Hostname, &v.InterfaceSelector, &v.CollectionError, &v.Enabled, &v.ListenPort, &v.MTU, &v.Address, &v.Endpoint, &v.Region, &v.LocationName, &v.LocationSource, &v.Latitude, &v.Longitude, &v.OS, &v.AgentVersion, &labels, &v.PublicKey, &lastSeen, &created); err != nil {
		return Node{}, notFound(err)
	}
	if err := json.Unmarshal([]byte(labels), &v.Labels); err != nil {
		return Node{}, err
	}
	if v.Labels == nil {
		v.Labels = map[string]string{}
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
