package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

var errDatabaseNotConfigured = errors.New("database is not configured")

// DatabaseConfig is accepted only by the unauthenticated first-run setup API.
// Password is encrypted before it is written to the bootstrap configuration file.
type DatabaseConfig struct {
	Driver     string `json:"driver"`
	SQLitePath string `json:"sqlite_path,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Database   string `json:"database,omitempty"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	SSLMode    string `json:"ssl_mode,omitempty"`
}

type DatabaseStatus struct {
	Configured bool   `json:"configured"`
	Driver     string `json:"driver,omitempty"`
}

type storedDatabaseConfig struct {
	Version int             `json:"version"`
	Driver  string          `json:"driver"`
	Secret  EncryptedSecret `json:"secret"`
}

// DatabaseManager owns the switchable store used during first-run setup.
type DatabaseManager struct {
	mu         sync.Mutex
	store      *SwitchableStore
	box        *SecretBox
	configPath string
	configured bool
	driver     string
	active     *SQLStore
	retired    []*SQLStore
}

func NewDatabaseManager(configPath, masterKey string) (*DatabaseManager, error) {
	box, err := NewSecretBox(masterKey)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve database configuration path: %w", err)
	}
	manager := &DatabaseManager{
		store:      NewSwitchableStore(NewMemoryStore()),
		box:        box,
		configPath: absolute,
	}
	if err := manager.load(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *DatabaseManager) Store() Store { return m.store }

func (m *DatabaseManager) Status() DatabaseStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return DatabaseStatus{Configured: m.configured, Driver: m.driver}
}

func (m *DatabaseManager) Test(ctx context.Context, cfg DatabaseConfig) error {
	_, dsn, err := normalizeDatabaseConfig(cfg, filepath.Dir(m.configPath))
	if err != nil {
		return err
	}
	return testSQLConnection(ctx, cfg.Driver, dsn)
}

func (m *DatabaseManager) Configure(ctx context.Context, cfg DatabaseConfig) (DatabaseStatus, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if initialized, err := m.store.HasUsers(); err != nil {
		return DatabaseStatus{}, false, fmt.Errorf("read current setup state: %w", err)
	} else if initialized {
		return DatabaseStatus{}, true, errAlreadyInitialized
	}

	normalized, dsn, err := normalizeDatabaseConfig(cfg, filepath.Dir(m.configPath))
	if err != nil {
		return DatabaseStatus{}, false, err
	}
	store, err := OpenSQLStore(normalized.Driver, dsn)
	if err != nil {
		return DatabaseStatus{}, false, fmt.Errorf("database connection or table creation failed: %w", err)
	}
	initialized, err := store.HasUsers()
	if err != nil {
		store.Close()
		return DatabaseStatus{}, false, fmt.Errorf("verify initialized database: %w", err)
	}
	if err := m.save(normalized); err != nil {
		store.Close()
		return DatabaseStatus{}, false, err
	}
	if m.active != nil {
		m.retired = append(m.retired, m.active)
	}
	m.active = store
	m.store.Switch(store)
	m.configured = true
	m.driver = normalized.Driver
	return DatabaseStatus{Configured: true, Driver: normalized.Driver}, initialized, nil
}

func (m *DatabaseManager) CreateInitialAdmin(user User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.configured {
		return errDatabaseNotConfigured
	}
	return m.store.CreateInitialAdmin(user)
}

func (m *DatabaseManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for _, store := range append(m.retired, m.active) {
		if store != nil {
			if err := store.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	m.retired = nil
	m.active = nil
	return first
}

func (m *DatabaseManager) load() error {
	raw, err := os.ReadFile(m.configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read database configuration: %w", err)
	}
	var stored storedDatabaseConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		return fmt.Errorf("decode database configuration: %w", err)
	}
	plaintext, err := m.box.Decrypt(stored.Secret)
	if err != nil {
		return fmt.Errorf("decrypt database configuration: %w", err)
	}
	var cfg DatabaseConfig
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return fmt.Errorf("decode encrypted database configuration: %w", err)
	}
	normalized, dsn, err := normalizeDatabaseConfig(cfg, filepath.Dir(m.configPath))
	if err != nil {
		return fmt.Errorf("validate saved database configuration: %w", err)
	}
	store, err := OpenSQLStore(normalized.Driver, dsn)
	if err != nil {
		return fmt.Errorf("open configured database: %w", err)
	}
	m.active = store
	m.store.Switch(store)
	m.configured = true
	m.driver = normalized.Driver
	return nil
}

func (m *DatabaseManager) save(cfg DatabaseConfig) error {
	plaintext, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode database configuration: %w", err)
	}
	secret, err := m.box.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt database configuration: %w", err)
	}
	raw, err := json.MarshalIndent(storedDatabaseConfig{Version: 1, Driver: cfg.Driver, Secret: secret}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode stored database configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0700); err != nil {
		return fmt.Errorf("create database configuration directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(m.configPath), ".wiremesh-database-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary database configuration: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return fmt.Errorf("protect database configuration: %w", err)
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return fmt.Errorf("write database configuration: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync database configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close database configuration: %w", err)
	}
	if err := os.Rename(tempName, m.configPath); err != nil {
		if removeErr := os.Remove(m.configPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace database configuration: %w", err)
		}
		if err := os.Rename(tempName, m.configPath); err != nil {
			return fmt.Errorf("store database configuration: %w", err)
		}
	}
	return nil
}

func normalizeDatabaseConfig(cfg DatabaseConfig, baseDir string) (DatabaseConfig, string, error) {
	cfg.Driver = strings.ToLower(strings.TrimSpace(cfg.Driver))
	switch cfg.Driver {
	case "sqlite", "sqlite3":
		cfg.Driver = "sqlite"
		cfg.SQLitePath = strings.TrimSpace(cfg.SQLitePath)
		if cfg.SQLitePath == "" {
			cfg.SQLitePath = "wiremesh.db"
		}
		resolved := cfg.SQLitePath
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(baseDir, resolved)
		}
		resolved, err := filepath.Abs(resolved)
		if err != nil {
			return DatabaseConfig{}, "", fmt.Errorf("resolve SQLite path: %w", err)
		}
		baseAbsolute, err := filepath.Abs(baseDir)
		if err != nil {
			return DatabaseConfig{}, "", fmt.Errorf("resolve database data directory: %w", err)
		}
		relative, err := filepath.Rel(baseAbsolute, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return DatabaseConfig{}, "", errors.New("SQLite path must stay inside the WireMesh data directory")
		}
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			return DatabaseConfig{}, "", errors.New("SQLite 路径必须是文件")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return DatabaseConfig{}, "", fmt.Errorf("inspect SQLite path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(resolved), 0700); err != nil {
			return DatabaseConfig{}, "", fmt.Errorf("create SQLite directory: %w", err)
		}
		query := url.Values{}
		query.Add("_pragma", "busy_timeout(5000)")
		query.Add("_pragma", "journal_mode(WAL)")
		query.Add("_pragma", "foreign_keys(1)")
		// modernc SQLite expects file:C:/path on Windows; file:///C:/path is
		// interpreted as a URI authority and rejected.
		sqlitePath := strings.ReplaceAll(filepath.ToSlash(resolved), "#", "%23")
		sqlitePath = strings.ReplaceAll(sqlitePath, "?", "%3F")
		cfg.Host, cfg.Database, cfg.Username, cfg.Password, cfg.SSLMode = "", "", "", "", ""
		cfg.Port = 0
		return cfg, "file:" + sqlitePath + "?" + query.Encode(), nil
	case "postgres", "postgresql", "pgsql", "pgx":
		cfg.Driver = "postgres"
		if err := validateRemoteConfig(&cfg, 5432, "prefer"); err != nil {
			return DatabaseConfig{}, "", err
		}
		if !oneOf(cfg.SSLMode, "disable", "allow", "prefer", "require", "verify-ca", "verify-full") {
			return DatabaseConfig{}, "", errors.New("unsupported PostgreSQL SSL mode")
		}
		u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Path: "/" + cfg.Database}
		u.User = url.UserPassword(cfg.Username, cfg.Password)
		q := u.Query()
		q.Set("sslmode", cfg.SSLMode)
		u.RawQuery = q.Encode()
		cfg.SQLitePath = ""
		return cfg, u.String(), nil
	case "mysql":
		if err := validateRemoteConfig(&cfg, 3306, "preferred"); err != nil {
			return DatabaseConfig{}, "", err
		}
		tlsMode := map[string]string{"disable": "false", "preferred": "preferred", "require": "true", "skip-verify": "skip-verify"}[cfg.SSLMode]
		if tlsMode == "" {
			return DatabaseConfig{}, "", errors.New("unsupported MySQL TLS mode")
		}
		mysqlCfg := mysqlDriver.NewConfig()
		mysqlCfg.User = cfg.Username
		mysqlCfg.Passwd = cfg.Password
		mysqlCfg.Net = "tcp"
		mysqlCfg.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
		mysqlCfg.DBName = cfg.Database
		mysqlCfg.ParseTime = true
		mysqlCfg.Loc = time.UTC
		mysqlCfg.TLSConfig = tlsMode
		mysqlCfg.Params = map[string]string{"charset": "utf8mb4"}
		cfg.SQLitePath = ""
		return cfg, mysqlCfg.FormatDSN(), nil
	default:
		return DatabaseConfig{}, "", fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}
}

func validateRemoteConfig(cfg *DatabaseConfig, defaultPort int, defaultSSL string) error {
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Database = strings.TrimSpace(cfg.Database)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.SSLMode = strings.ToLower(strings.TrimSpace(cfg.SSLMode))
	if cfg.Host == "" || strings.ContainsAny(cfg.Host, "/\\\r\n\t") {
		return errors.New("database host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("database port must be between 1 and 65535")
	}
	if cfg.Database == "" {
		return errors.New("database name is required")
	}
	if cfg.Username == "" {
		return errors.New("database username is required")
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = defaultSSL
	}
	return nil
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func testSQLConnection(ctx context.Context, driver, dsn string) error {
	driver = strings.ToLower(strings.TrimSpace(driver))
	sqlDriver := ""
	switch driver {
	case "sqlite", "sqlite3":
		sqlDriver = "sqlite"
	case "postgres", "postgresql", "pgsql", "pgx":
		sqlDriver = "pgx"
	case "mysql":
		sqlDriver = "mysql"
	default:
		return fmt.Errorf("unsupported database driver %q", driver)
	}
	db, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if sqlDriver == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(pingCtx)
}

func (a *App) databaseStatus(w http.ResponseWriter, r *http.Request) {
	if a.database == nil {
		writeJSON(w, http.StatusOK, DatabaseStatus{Configured: true, Driver: a.databaseDriver})
		return
	}
	writeJSON(w, http.StatusOK, a.database.Status())
}

func (a *App) testDatabase(w http.ResponseWriter, r *http.Request) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "数据库由服务端环境变量管理")
		return
	}
	var cfg DatabaseConfig
	if !decode(w, r, &cfg) {
		return
	}
	if initialized, err := a.store.HasUsers(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取初始化状态失败")
		return
	} else if initialized {
		writeError(w, http.StatusConflict, "WireMesh 已完成初始化")
		return
	}
	if err := a.database.Test(r.Context(), cfg); err != nil {
		writeError(w, http.StatusBadRequest, "数据库连接失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func (a *App) configureDatabase(w http.ResponseWriter, r *http.Request) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "数据库由服务端环境变量管理")
		return
	}
	var cfg DatabaseConfig
	if !decode(w, r, &cfg) {
		return
	}
	status, initialized, err := a.database.Configure(r.Context(), cfg)
	if err != nil {
		if errors.Is(err, errAlreadyInitialized) {
			writeError(w, http.StatusConflict, "WireMesh 已完成初始化")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": status.Configured, "driver": status.Driver, "initialized": initialized})
}
