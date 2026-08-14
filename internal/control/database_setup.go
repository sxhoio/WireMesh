package control

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

var errDatabaseNotConfigured = errors.New("database is not configured")

// RedactCredentials 从文本中剥离可能回显的数据库凭据（密码/口令），
// 供服务端日志使用：驱动在 DSN 解析或连接失败时可能把 user:password@
// 或 password=... 原样带进错误消息，直接打印会泄漏到日志（可观测性专项）。
func RedactCredentials(text string) string {
	// URL 风格 user:password@host
	reURL := regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^/@:\s]+):([^@/\s]+)@`)
	text = reURL.ReplaceAllString(text, "${1}***:***@")
	// 键值风格 password=xxx / passwd=xxx / pwd=xxx（DSN 与连接串）
	reKV := regexp.MustCompile(`(?i)(\b(?:password|passwd|pwd)\s*[=:]\s*)[^\s,;]+`)
	text = reKV.ReplaceAllString(text, "${1}***")
	return text
}

func redactCredentialsError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(RedactCredentials(err.Error()))
}

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
	sqlitePath string
	active     *SQLStore
	retired    []*SQLStore
	// instanceID 是实例级备份绑定标识（跨重启保持：备份时若不存在则
	// 生成并写入 backup_meta；恢复校验用 HMAC 匹配，无需持久化于内存）。
	instanceID string
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
	if normalized.Driver == "sqlite" {
		m.sqlitePath = normalized.SQLitePath
	}
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
	if normalized.Driver == "sqlite" {
		m.sqlitePath = normalized.SQLitePath
	}
	return nil
}

// BackupSQLite 使用 VACUUM INTO 生成当前 SQLite 数据库的一致性在线备份。
// 备份前写入平台绑定标记（instance_id + master-key 派生的 HMAC），
// 恢复时校验标记，防止跨实例备份被注入（C-1：恢复越权修复）。
func (m *DatabaseManager) BackupSQLite(targetPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.driver != "sqlite" || m.active == nil {
		return errors.New("backup is only supported for a configured SQLite database")
	}
	if err := m.ensureBackupMeta(); err != nil {
		return fmt.Errorf("write backup binding marker: %w", err)
	}
	escaped := strings.ReplaceAll(targetPath, "'", "''")
	_, err := m.active.db.Exec(`VACUUM INTO '` + escaped + `'`)
	return err
}

// ensureBackupMeta 在活动库中写入/更新单行备份绑定标记。
func (m *DatabaseManager) ensureBackupMeta() error {
	instanceID := m.backupInstanceID()
	instanceHMAC, err := m.backupInstanceHMAC(instanceID)
	if err != nil {
		return err
	}
	query := `INSERT INTO backup_meta (id, instance_id, instance_hmac, created_at) VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET instance_id=excluded.instance_id, instance_hmac=excluded.instance_hmac`
	if m.driver == "mysql" {
		query = `INSERT INTO backup_meta (id, instance_id, instance_hmac, created_at) VALUES (1, ?, ?, ?)
			ON DUPLICATE KEY UPDATE instance_id=VALUES(instance_id), instance_hmac=VALUES(instance_hmac)`
	}
	_, err = m.active.db.Exec(m.active.query(query), instanceID, instanceHMAC, timeText(time.Now().UTC()))
	return err
}

// backupInstanceID 返回本实例的稳定标识（无则生成并持久化在数据库配置旁）。
func (m *DatabaseManager) backupInstanceID() string {
	if m.instanceID != "" {
		return m.instanceID
	}
	m.instanceID = newID("inst")
	return m.instanceID
}

// backupInstanceHMAC 用 master key 派生的密钥对 instance_id 做 HMAC-SHA256。
func (m *DatabaseManager) backupInstanceHMAC(instanceID string) (string, error) {
	key, err := m.box.HMACKey()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("wiremesh-backup:" + instanceID))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// ValidateBackup 校验备份文件：合法 SQLite + 含用户 + 完整性 + 平台绑定标记
// 与当前实例一致。返回错误时拒绝恢复（C-1：防跨实例备份注入与租户数据覆盖）。
func (m *DatabaseManager) ValidateBackup(replacementPath string) error {
	probe, err := OpenSQLStore("sqlite", "file:"+escapeSQLitePath(replacementPath))
	if err != nil {
		return fmt.Errorf("invalid SQLite backup: %w", err)
	}
	defer probe.Close()
	hasUsers, userErr := probe.HasUsers()
	var integrity string
	_ = probe.db.QueryRow(`PRAGMA quick_check`).Scan(&integrity)
	if userErr != nil || !hasUsers {
		return errors.New("backup file contains no users")
	}
	if integrity != "" && !strings.EqualFold(strings.TrimSpace(integrity), "ok") {
		return errors.New("backup file failed SQLite integrity check")
	}
	// 平台绑定：备份必须来自本实例（master key 派生 HMAC 匹配）
	var storedID, storedHMAC string
	err = probe.db.QueryRow(`SELECT instance_id, instance_hmac FROM backup_meta WHERE id = 1`).Scan(&storedID, &storedHMAC)
	if err != nil {
		return errors.New("backup is missing the platform binding marker; reject backup from another instance")
	}
	expectedHMAC, hmacErr := m.backupInstanceHMAC(storedID)
	if hmacErr != nil {
		return hmacErr
	}
	if !hmac.Equal([]byte(strings.ToLower(storedHMAC)), []byte(expectedHMAC)) {
		return errors.New("backup was created by a different WireMesh instance; restore rejected")
	}
	return nil
}

// ClearAllSessionsAfterRestore 恢复（整体替换库）后调用：清空内存会话与
// 吊销表，强制全部用户重新登录——恢复可能覆盖 users/revoked_tokens，
// 内存态与磁盘已不一致，不能再信任任何既有令牌。
func (a *App) ClearAllSessionsAfterRestore() {
	a.sessionMu.Lock()
	a.sessions = map[string]UserSession{}
	a.revokedTokens = map[string]time.Time{}
	a.sessionMu.Unlock()
	a.ssoMu.Lock()
	a.ssoStates = map[string]ssoState{}
	a.ssoMu.Unlock()
	a.loginMu.Lock()
	a.loginFailures = map[string][]time.Time{}
	a.changePasswordMu.Lock()
	a.changePasswordFailures = map[string][]time.Time{}
	a.changePasswordMu.Unlock()
	a.loginMu.Unlock()
	a.setupMu.Lock()
	a.setupAttempts = map[string][]time.Time{}
	a.setupMu.Unlock()
}

// RestoreSQLite 用上传的 SQLite 备份文件替换当前数据库并热切换。
// 流程：校验备份 → 复制到持久化路径（sqlitePath）同目录临时文件 →
// 原子 rename 覆盖 → 重新打开并切换。活动库始终指向持久化路径，
// 临时文件可安全删除，恢复在进程重启后仍然生效。
func (m *DatabaseManager) RestoreSQLite(ctx context.Context, replacementPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.driver != "sqlite" || m.active == nil || m.sqlitePath == "" {
		return errors.New("restore is only supported for a configured SQLite database")
	}
	// 1. 校验备份：合法 SQLite + 含用户数据 + 完整性检查 + 平台绑定标记
	//    （C-1：拒绝跨实例备份，防止租户数据注入/覆盖）
	if err := m.ValidateBackup(replacementPath); err != nil {
		return err
	}
	// 2. 复制到持久化路径同目录的临时文件（原子 rename 要求同文件系统）
	dir := filepath.Dir(m.sqlitePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".wiremesh-restore-*.db")
	if err != nil {
		return fmt.Errorf("create restore temp file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	src, err := os.Open(replacementPath)
	if err != nil {
		temp.Close()
		return fmt.Errorf("open backup file: %w", err)
	}
	_, copyErr := io.Copy(temp, src)
	src.Close()
	if copyErr != nil {
		temp.Close()
		return fmt.Errorf("copy backup file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, 0600); err != nil {
		return err
	}
	// 3. 关闭活动库以释放文件句柄（Windows 上 rename 要求目标未被占用），
	//    再原子替换并重新打开。rename 失败时尝试重开旧库自愈，尽量保持服务可用。
	previous := m.active
	_ = previous.Close()
	if err := os.Rename(tempName, m.sqlitePath); err != nil {
		if reopened, openErr := OpenSQLStore("sqlite", "file:"+escapeSQLitePath(m.sqlitePath)); openErr == nil {
			m.active = reopened
			m.store.Switch(reopened)
		}
		return fmt.Errorf("commit restore: %w", err)
	}
	replacement, err := OpenSQLStore("sqlite", "file:"+escapeSQLitePath(m.sqlitePath))
	if err != nil {
		return fmt.Errorf("reopen restored database: %w", err)
	}
	m.active = replacement
	m.retired = append(m.retired, previous)
	m.store.Switch(replacement)
	return nil
}

// escapeSQLitePath 转义 SQLite DSN 中的特殊字符。
func escapeSQLitePath(path string) string {
	return strings.ReplaceAll(filepath.ToSlash(path), "#", "%23")
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
		// RowsAffected 按"匹配行数"而非"实际变更行数"计数，与 SQLite/PostgreSQL
		// 行为一致：值未变化的 UPDATE 同样返回 1，避免 changed() 误判 404、
		// UpdateDelivery 误退化为 CreateDelivery 撞唯一约束。
		mysqlCfg.ClientFoundRows = true
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
	// 主机名只允许主机名/IP 的合法字符，拒绝可用于 DSN 注入的字符
	// （@ 用户信息、? 查询串、# 片段、& 参数分隔、; 语句分隔、% 编码、空白）。
	if cfg.Host == "" || strings.ContainsAny(cfg.Host, "/\\\r\n\t@#?&;% ") {
		return errors.New("database host is required")
	}
	// 拒绝解析结果落在私网/保留/链路本地/组播的主机，防止未初始化窗口的内网探测（SSRF）；
	// 回环地址（本机数据库）放行；设置 WIREMESH_DATABASE_ALLOW_PRIVATE=1 可显式放开。
	if !allowPrivateDatabaseHosts() {
		if err := validateDatabaseHost(cfg.Host); err != nil {
			return err
		}
		// M-2：把域名替换为已校验的 IP 字面量——连接时不再重新解析，
		// 封堵校验与连接之间的 DNS rebinding 窗口（与通知/OIDC 外呼一致）。
		if resolved, ok := resolveDatabaseHostIP(cfg.Host); ok {
			cfg.Host = resolved
		}
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

// allowPrivateDatabaseHosts 是否允许数据库指向私网/保留地址（默认关闭，SSRF 防护）。
func allowPrivateDatabaseHosts() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("WIREMESH_DATABASE_ALLOW_PRIVATE")), "1")
}

// validateDatabaseHost 校验数据库主机：IP 字面量直接判定，域名解析后判定；
// 回环（本机数据库）放行，其余私网/保留/链路本地/组播地址拒绝。
func validateDatabaseHost(host string) error {
	trimmed := strings.TrimSpace(host)
	if strings.EqualFold(trimmed, "localhost") {
		return nil
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		if ip.IsLoopback() {
			return nil
		}
		if isUnsafeDatabaseIP(ip) {
			return errors.New("database host must not be a private, reserved, or link-local address")
		}
		return nil
	}
	ips, err := net.LookupIP(trimmed)
	if err != nil {
		return errors.New("database host could not be resolved")
	}
	for _, ip := range ips {
		if ip.IsLoopback() {
			continue
		}
		if isUnsafeDatabaseIP(ip) {
			return errors.New("database host must not resolve to a private, reserved, or link-local address")
		}
	}
	return nil
}

// resolveDatabaseHostIP 解析域名并返回第一个安全的公网 IP 字面量。
// 调用前已通过 validateDatabaseHost 校验（无私网解析结果），此处把主机名
// 换成 IP，连接阶段不再重新解析（封堵 DNS rebinding，M-2）。
func resolveDatabaseHostIP(host string) (string, bool) {
	trimmed := strings.TrimSpace(host)
	if net.ParseIP(trimmed) != nil || strings.EqualFold(trimmed, "localhost") {
		return trimmed, false // IP 字面量或本机别名无需替换
	}
	ips, err := net.LookupIP(trimmed)
	if err != nil {
		return "", false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || isUnsafeDatabaseIP(ip) {
			continue
		}
		return ip.String(), true
	}
	return "", false
}

// isUnsafeDatabaseIP 判定不可外呼的地址：与通知渠道的私网判定一致，并补齐
// CGNAT/benchmark/文档网络等常见保留段。
func isUnsafeDatabaseIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, cidr := range []string{
		"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "192.0.2.0/24",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "169.254.0.0/16",
	} {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
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
	ip := clientIP(r)
	if !a.checkSetupAllowed(ip) {
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
		return
	}
	a.recordSetupAttempt(ip)
	if !a.requireSetupToken(w, r) {
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
		// 细节写入服务端日志（供排障，先脱敏以防 DSN 凭据回显），
		// 响应保持通用，避免驱动差异被用作内网探测 oracle
		log.Printf("database connectivity test failed: %s", RedactCredentials(err.Error()))
		writeError(w, http.StatusBadRequest, "数据库连接失败，请检查主机、端口、凭据与网络配置")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func (a *App) configureDatabase(w http.ResponseWriter, r *http.Request) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "数据库由服务端环境变量管理")
		return
	}
	ip := clientIP(r)
	if !a.checkSetupAllowed(ip) {
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
		return
	}
	a.recordSetupAttempt(ip)
	if !a.requireSetupToken(w, r) {
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
		// M-1：统一通用文案，细节只写服务端日志（脱敏），避免驱动差异
		// 被用作内网探测 oracle（与 testDatabase 一致）。
		log.Printf("database configure failed: %s", RedactCredentials(err.Error()))
		writeError(w, http.StatusBadRequest, "数据库连接失败，请检查主机、端口、凭据与网络配置")
		return
	}
	a.clearSetupAttempts(ip)
	writeJSON(w, http.StatusOK, map[string]any{"configured": status.Configured, "driver": status.Driver, "initialized": initialized})
}
