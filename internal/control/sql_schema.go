package control

import (
	"context"
	"fmt"
	"strings"
)

type schemaStatement struct {
	common string
	mysql  string
}

var controlSchemaStatements = []schemaStatement{
	{common: `CREATE TABLE IF NOT EXISTS setup_locks (name TEXT PRIMARY KEY)`, mysql: `CREATE TABLE IF NOT EXISTS setup_locks (name VARCHAR(191) PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL, last_login_at TEXT, created_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS users (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, email VARCHAR(320) NOT NULL UNIQUE, password_hash VARCHAR(255) NOT NULL, name VARCHAR(255) NOT NULL, role VARCHAR(32) NOT NULL, last_login_at VARCHAR(40), created_at VARCHAR(40) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, created_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS projects (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, description TEXT NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX projects_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS projects_tenant_idx ON projects (tenant_id, created_at)`},
	{common: `CREATE TABLE IF NOT EXISTS networks (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, name TEXT NOT NULL, cidr TEXT NOT NULL, dns TEXT NOT NULL, topology TEXT NOT NULL, created_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS networks (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, cidr VARCHAR(64) NOT NULL, dns TEXT NOT NULL, topology VARCHAR(32) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX networks_project_idx (tenant_id, project_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS networks_project_idx ON networks (tenant_id, project_id)`},
	{common: `CREATE TABLE IF NOT EXISTS nodes (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, network_id TEXT NOT NULL, name TEXT NOT NULL, hostname TEXT NOT NULL DEFAULT '', interface_selector TEXT NOT NULL DEFAULT '', collection_error TEXT NOT NULL DEFAULT '', enabled BOOLEAN NOT NULL DEFAULT TRUE, listen_port INTEGER NOT NULL DEFAULT 51820, mtu INTEGER NOT NULL DEFAULT 1420, address TEXT NOT NULL, endpoint TEXT NOT NULL, region TEXT NOT NULL, location_name VARCHAR(255) NOT NULL DEFAULT '', location_source VARCHAR(32) NOT NULL DEFAULT '', latitude DOUBLE PRECISION NOT NULL DEFAULT 0, longitude DOUBLE PRECISION NOT NULL DEFAULT 0, os TEXT NOT NULL, agent_version TEXT NOT NULL, labels_json TEXT NOT NULL, public_key TEXT NOT NULL, private_key_json TEXT NOT NULL, wireguard_json TEXT NOT NULL, peer_config_json TEXT NOT NULL DEFAULT '[]', desired_peer_config_json TEXT NOT NULL DEFAULT '[]', last_seen TEXT NOT NULL, created_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS nodes (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, hostname VARCHAR(255) NOT NULL DEFAULT '', interface_selector VARCHAR(255) NOT NULL DEFAULT '', collection_error LONGTEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE, listen_port INTEGER NOT NULL DEFAULT 51820, mtu INTEGER NOT NULL DEFAULT 1420, address VARCHAR(64) NOT NULL, endpoint TEXT NOT NULL, region VARCHAR(255) NOT NULL, location_name VARCHAR(255) NOT NULL DEFAULT '', location_source VARCHAR(32) NOT NULL DEFAULT '', latitude DOUBLE PRECISION NOT NULL DEFAULT 0, longitude DOUBLE PRECISION NOT NULL DEFAULT 0, os VARCHAR(255) NOT NULL, agent_version VARCHAR(255) NOT NULL, labels_json LONGTEXT NOT NULL, public_key TEXT NOT NULL, private_key_json LONGTEXT NOT NULL, wireguard_json LONGTEXT NOT NULL, peer_config_json LONGTEXT NULL, desired_peer_config_json LONGTEXT NULL, last_seen VARCHAR(40) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX nodes_network_idx (tenant_id, network_id), UNIQUE INDEX nodes_address_idx (network_id, address)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS nodes_network_idx ON nodes (tenant_id, network_id)`},
	{common: `CREATE UNIQUE INDEX IF NOT EXISTS nodes_address_idx ON nodes (network_id, address)`},
	{common: `CREATE TABLE IF NOT EXISTS traffic_samples (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, node_id TEXT NOT NULL, interface_name TEXT NOT NULL, receive_bytes BIGINT NOT NULL, transmit_bytes BIGINT NOT NULL, recorded_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS traffic_samples (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, node_id VARCHAR(191) NOT NULL, interface_name VARCHAR(191) NOT NULL, receive_bytes BIGINT NOT NULL, transmit_bytes BIGINT NOT NULL, recorded_at VARCHAR(40) NOT NULL, INDEX traffic_samples_node_idx (tenant_id, node_id, interface_name, recorded_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS traffic_samples_node_idx ON traffic_samples (tenant_id, node_id, interface_name, recorded_at)`},
	{common: `CREATE TABLE IF NOT EXISTS peer_relations (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, network_id TEXT NOT NULL, source_node_id TEXT NOT NULL, target_node_id TEXT NOT NULL, created_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS peer_relations (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, source_node_id VARCHAR(191) NOT NULL, target_node_id VARCHAR(191) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX peers_network_idx (tenant_id, network_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS peers_network_idx ON peer_relations (tenant_id, network_id)`},
	{common: `CREATE TABLE IF NOT EXISTS config_revisions (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, network_id TEXT NOT NULL, version BIGINT NOT NULL, configs_json TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE (network_id, version))`, mysql: `CREATE TABLE IF NOT EXISTS config_revisions (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, version BIGINT NOT NULL, configs_json LONGTEXT NOT NULL, created_at VARCHAR(40) NOT NULL, UNIQUE INDEX revisions_network_version_idx (network_id, version)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE TABLE IF NOT EXISTS config_deliveries (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, node_id TEXT NOT NULL, version BIGINT NOT NULL, state TEXT NOT NULL, message TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE (tenant_id, node_id, version))`, mysql: `CREATE TABLE IF NOT EXISTS config_deliveries (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, node_id VARCHAR(191) NOT NULL, version BIGINT NOT NULL, state VARCHAR(64) NOT NULL, message TEXT NOT NULL, updated_at VARCHAR(40) NOT NULL, UNIQUE INDEX deliveries_node_version_idx (tenant_id, node_id, version), INDEX deliveries_tenant_idx (tenant_id, updated_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS deliveries_tenant_idx ON config_deliveries (tenant_id, updated_at)`},
	{common: `CREATE TABLE IF NOT EXISTS agent_commands (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, node_id TEXT NOT NULL, type TEXT NOT NULL, state TEXT NOT NULL, result TEXT NOT NULL, created_at TEXT NOT NULL, started_at TEXT, completed_at TEXT)`, mysql: `CREATE TABLE IF NOT EXISTS agent_commands (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, node_id VARCHAR(191) NOT NULL, type VARCHAR(64) NOT NULL, state VARCHAR(32) NOT NULL, result LONGTEXT NOT NULL, created_at VARCHAR(40) NOT NULL, started_at VARCHAR(40) NULL, completed_at VARCHAR(40) NULL, INDEX agent_commands_node_idx (tenant_id, node_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS agent_commands_node_idx ON agent_commands (tenant_id, node_id, created_at)`},
	{common: `CREATE TABLE IF NOT EXISTS enrollment_tokens (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, project_id TEXT NOT NULL, network_id TEXT NOT NULL, token TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, used_at TEXT)`, mysql: `CREATE TABLE IF NOT EXISTS enrollment_tokens (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, project_id VARCHAR(191) NOT NULL, network_id VARCHAR(191) NOT NULL, token VARCHAR(255) NOT NULL UNIQUE, expires_at VARCHAR(40) NOT NULL, used_at VARCHAR(40) NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE TABLE IF NOT EXISTS agent_identities (node_id TEXT PRIMARY KEY, certificate_pem TEXT NOT NULL, certificate_fingerprint TEXT NOT NULL, expires_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS agent_identities (node_id VARCHAR(191) PRIMARY KEY, certificate_pem LONGTEXT NOT NULL, certificate_fingerprint VARCHAR(255) NOT NULL, expires_at VARCHAR(40) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE TABLE IF NOT EXISTS system_settings (tenant_id TEXT PRIMARY KEY, settings_json TEXT NOT NULL, geoip_db_path TEXT NOT NULL, updated_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS system_settings (tenant_id VARCHAR(191) PRIMARY KEY, settings_json LONGTEXT NOT NULL, geoip_db_path TEXT NOT NULL, updated_at VARCHAR(40) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE TABLE IF NOT EXISTS notification_channels (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, name TEXT NOT NULL, type TEXT NOT NULL, target_json TEXT NOT NULL, enabled INTEGER NOT NULL, all_agents INTEGER NOT NULL, agent_ids_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS notification_channels (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, name VARCHAR(255) NOT NULL, type VARCHAR(32) NOT NULL, target_json LONGTEXT NOT NULL, enabled BOOLEAN NOT NULL, all_agents BOOLEAN NOT NULL, agent_ids_json LONGTEXT NOT NULL, created_at VARCHAR(40) NOT NULL, updated_at VARCHAR(40) NOT NULL, INDEX notification_channels_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS notification_channels_tenant_idx ON notification_channels (tenant_id, created_at)`},
	{common: `CREATE TABLE IF NOT EXISTS notification_logs (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, channel_id TEXT NOT NULL, channel_name TEXT NOT NULL, channel_type TEXT NOT NULL, agent_name TEXT NOT NULL, message TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS notification_logs (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, channel_id VARCHAR(191) NOT NULL, channel_name VARCHAR(255) NOT NULL, channel_type VARCHAR(32) NOT NULL, agent_name VARCHAR(255) NOT NULL, message TEXT NOT NULL, status VARCHAR(32) NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX notification_logs_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS notification_logs_tenant_idx ON notification_logs (tenant_id, created_at)`},
	{common: `CREATE TABLE IF NOT EXISTS audit_events (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, metadata_json TEXT NOT NULL, created_at TEXT NOT NULL)`, mysql: `CREATE TABLE IF NOT EXISTS audit_events (id VARCHAR(191) PRIMARY KEY, tenant_id VARCHAR(191) NOT NULL, actor_id VARCHAR(191) NOT NULL, action VARCHAR(255) NOT NULL, resource_type VARCHAR(255) NOT NULL, resource_id VARCHAR(191) NOT NULL, metadata_json LONGTEXT NOT NULL, created_at VARCHAR(40) NOT NULL, INDEX audit_tenant_idx (tenant_id, created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
	{common: `CREATE INDEX IF NOT EXISTS audit_tenant_idx ON audit_events (tenant_id, created_at)`},
}

func buildSchemaStatements(driver string) []string {
	out := make([]string, 0, len(controlSchemaStatements))
	for _, statement := range controlSchemaStatements {
		if driver == "mysql" {
			if statement.mysql != "" {
				out = append(out, statement.mysql)
			}
			continue
		}
		out = append(out, statement.common)
	}
	return out
}

type schemaColumn struct {
	name                    string
	definition              string
	mysqlDefinition         string
	mysqlInitializeNullSQL  string
	mysqlInitializeNullDesc string
}

func (column schemaColumn) definitionFor(driver string) string {
	if driver == "mysql" && column.mysqlDefinition != "" {
		return column.mysqlDefinition
	}
	return column.definition
}

func (s *SQLStore) schemaColumnExists(ctx context.Context, table, column string) (bool, error) {
	var count int
	switch s.driver {
	case "sqlite":
		lookup := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?", strings.ReplaceAll(table, "'", "''"))
		if err := s.db.QueryRowContext(ctx, lookup, column).Scan(&count); err != nil {
			return false, err
		}
	case "mysql":
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&count); err != nil {
			return false, err
		}
	default:
		lookup := s.query(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`)
		if err := s.db.QueryRowContext(ctx, lookup, table, column).Scan(&count); err != nil {
			return false, err
		}
	}
	return count > 0, nil
}

func (s *SQLStore) ensureSchemaColumn(ctx context.Context, table string, column schemaColumn, inspectContext, addContext string) error {
	exists, err := s.schemaColumnExists(ctx, table, column.name)
	if err != nil {
		return fmt.Errorf("%s: %w", inspectContext, err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column.name+" "+column.definitionFor(s.driver)); err != nil {
		return fmt.Errorf("%s: %w", addContext, err)
	}
	if s.driver == "mysql" && column.mysqlInitializeNullSQL != "" {
		if _, err := s.db.ExecContext(ctx, column.mysqlInitializeNullSQL); err != nil {
			return fmt.Errorf("%s: %w", column.mysqlInitializeNullDesc, err)
		}
	}
	return nil
}

func (s *SQLStore) ensureNodeSchemaColumns(ctx context.Context, columns []schemaColumn) error {
	for _, column := range columns {
		if err := s.ensureSchemaColumn(
			ctx,
			"nodes",
			column,
			fmt.Sprintf("inspect nodes %s column", column.name),
			fmt.Sprintf("add nodes %s column", column.name),
		); err != nil {
			return err
		}
	}
	return nil
}
