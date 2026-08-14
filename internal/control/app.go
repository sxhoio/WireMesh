package control

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

type Config struct {
	MasterKey              string
	SetupToken             string
	Store                  Store
	Database               *DatabaseManager
	DatabaseDriver         string
	AgentBinaryPath        string
	AgentVersion           string
	CAFile                 string
	RequireAgentClientCert bool
}
type App struct {
	store                  Store
	database               *DatabaseManager
	databaseDriver         string
	box                    *SecretBox
	auth                   *Authenticator
	ca                     *x509.Certificate
	caKey                  any
	geoMu                  sync.RWMutex
	geoReaders             map[string]*geoReaderState
	geoFailures            map[string]time.Time
	geoLookup              func(string, string) (geoIPLocation, error)
	commandMu              sync.Mutex
	commandWakeups         map[string]chan struct{}
	sessionMu              sync.Mutex
	sessions               map[string]UserSession
	revokedTokens          map[string]time.Time
	ssoMu                  sync.Mutex
	ssoStates              map[string]ssoState
	agentBinaryPath        string
	agentVersion           string
	requireAgentClientCert bool
	loginMu                sync.Mutex
	loginFailures          map[string][]time.Time
	setupMu                sync.Mutex
	setupAttempts          map[string][]time.Time
	setupToken             string
}

func NewApp(cfg Config) (*App, error) {
	if strings.TrimSpace(cfg.MasterKey) == "" {
		return nil, errors.New("master key is required: set WIREMESH_MASTER_KEY to a long random secret")
	}
	box, err := NewSecretBox(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore()
	}
	app := &App{
		store:                  store,
		database:               cfg.Database,
		databaseDriver:         cfg.DatabaseDriver,
		box:                    box,
		geoReaders:             map[string]*geoReaderState{},
		geoFailures:            map[string]time.Time{},
		commandWakeups:         map[string]chan struct{}{},
		sessions:               map[string]UserSession{},
		revokedTokens:          map[string]time.Time{},
		ssoStates:              map[string]ssoState{},
		agentBinaryPath:        cfg.AgentBinaryPath,
		agentVersion:           strings.TrimSpace(cfg.AgentVersion),
		requireAgentClientCert: cfg.RequireAgentClientCert,
		loginFailures:          map[string][]time.Time{},
		setupAttempts:          map[string][]time.Time{},
		setupToken:             strings.TrimSpace(cfg.SetupToken),
	}
	app.geoLookup = app.lookupGeoIPLocation
	app.auth = newAuthenticator(store, cfg.MasterKey+"-auth")
	if cfg.CAFile != "" {
		// 生产部署：CA 私钥用 master key 加密持久化，重启后复用，避免吊销全部 Agent 证书
		if err := app.loadOrCreateCA(cfg.CAFile); err != nil {
			return nil, err
		}
	} else if err := app.newCertificateAuthority(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /metrics", a.metrics)
	mux.HandleFunc("GET /api/v1/setup/status", a.setupStatus)
	mux.HandleFunc("GET /api/v1/setup/database", a.databaseStatus)
	mux.HandleFunc("POST /api/v1/setup/database/test", a.testDatabase)
	mux.HandleFunc("POST /api/v1/setup/database", a.configureDatabase)
	mux.HandleFunc("POST /api/v1/setup", a.setup)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.HandleFunc("POST /api/v1/auth/change-password", a.withUser(RoleViewer, a.changePassword))
	mux.HandleFunc("GET /api/v1/auth/sessions", a.withUser(RoleAdmin, a.userSessions))
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", a.withUser(RoleAdmin, a.userSessions))
	mux.HandleFunc("GET /api/v1/auth/mfa/status", a.withUser(RoleViewer, a.mfaStatus))
	mux.HandleFunc("POST /api/v1/auth/mfa/setup", a.withUser(RoleViewer, a.mfaSetup))
	mux.HandleFunc("POST /api/v1/auth/mfa/enable", a.withUser(RoleViewer, a.mfaEnable))
	mux.HandleFunc("POST /api/v1/auth/mfa/disable", a.withUser(RoleViewer, a.mfaDisable))
	mux.HandleFunc("GET /api/v1/settings/sso", a.withUser(RoleAdmin, a.ssoConfig))
	mux.HandleFunc("PUT /api/v1/settings/sso", a.withUser(RoleAdmin, a.ssoConfig))
	mux.HandleFunc("GET /api/v1/auth/sso/login", a.ssoLogin)
	mux.HandleFunc("GET /api/v1/auth/sso/callback", a.ssoCallback)
	mux.HandleFunc("GET /api/v1/auth/me", a.withUser(RoleViewer, a.me))
	mux.HandleFunc("GET /api/v1/projects", a.withUser(RoleViewer, a.projects))
	mux.HandleFunc("POST /api/v1/projects", a.withUser(RoleAdmin, a.projects))
	mux.HandleFunc("GET /api/v1/networks", a.withUser(RoleViewer, a.networks))
	mux.HandleFunc("POST /api/v1/networks", a.withUser(RoleOperator, a.networks))
	mux.HandleFunc("GET /api/v1/nodes", a.withUser(RoleViewer, a.nodes))
	mux.HandleFunc("POST /api/v1/nodes", a.withUser(RoleOperator, a.nodes))
	mux.HandleFunc("GET /api/v1/nodes/{id}", a.withUser(RoleViewer, a.nodeByID))
	mux.HandleFunc("PATCH /api/v1/nodes/{id}", a.withUser(RoleOperator, a.updateNode))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", a.withUser(RoleAdmin, a.deleteNode))
	mux.HandleFunc("GET /api/v1/nodes/{id}/peer-config", a.withUser(RoleViewer, a.nodePeerConfig))
	mux.HandleFunc("GET /api/v1/nodes/{id}/client-config", a.withUser(RoleViewer, a.nodeClientConfig))
	mux.HandleFunc("PUT /api/v1/nodes/{id}/peer-config", a.withUser(RoleOperator, a.updateNodePeerConfig))
	mux.HandleFunc("POST /api/v1/nodes/{id}/collect", a.withUser(RoleOperator, a.createNodeCommand(agentCommandTypeCollect)))
	mux.HandleFunc("POST /api/v1/nodes/collect", a.withUser(RoleOperator, a.collectNodes))
	mux.HandleFunc("POST /api/v1/nodes/{id}/update-agent", a.withUser(RoleOperator, a.updateAgent))
	mux.HandleFunc("POST /api/v1/nodes/update-agent", a.withUser(RoleOperator, a.updateAgents))
	mux.HandleFunc("POST /api/v1/nodes/{id}/connectivity-check", a.withUser(RoleOperator, a.createNodeCommand(agentCommandTypeConnectivityCheck)))
	mux.HandleFunc("POST /api/v1/nodes/{id}/rotate-key", a.withUser(RoleAdmin, a.rotateNodeKey))
	mux.HandleFunc("GET /api/v1/nodes/{id}/logs", a.withUser(RoleViewer, a.nodeLogs))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}/logs", a.withUser(RoleOperator, a.clearNodeLogs))
	mux.HandleFunc("GET /api/v1/nodes/{id}/traffic", a.withUser(RoleViewer, a.nodeTraffic))
	mux.HandleFunc("GET /api/v1/networks/{id}/peers", a.withUser(RoleViewer, a.networkPeers))
	mux.HandleFunc("POST /api/v1/networks/{id}/peers", a.withUser(RoleOperator, a.addPeer))
	mux.HandleFunc("GET /api/v1/networks/{id}/access-resources", a.withUser(RoleViewer, a.accessResources))
	mux.HandleFunc("POST /api/v1/networks/{id}/access-resources", a.withUser(RoleOperator, a.accessResources))
	mux.HandleFunc("PUT /api/v1/networks/{id}/access-resources/{resource_id}", a.withUser(RoleOperator, a.updateAccessResource))
	mux.HandleFunc("DELETE /api/v1/networks/{id}/access-resources/{resource_id}", a.withUser(RoleOperator, a.deleteAccessResource))
	mux.HandleFunc("GET /api/v1/networks/{id}/access-policies", a.withUser(RoleViewer, a.accessPolicies))
	mux.HandleFunc("POST /api/v1/networks/{id}/access-policies", a.withUser(RoleOperator, a.accessPolicies))
	mux.HandleFunc("PUT /api/v1/networks/{id}/access-policies/{policy_id}", a.withUser(RoleOperator, a.updateAccessPolicy))
	mux.HandleFunc("DELETE /api/v1/networks/{id}/access-policies/{policy_id}", a.withUser(RoleOperator, a.deleteAccessPolicy))
	mux.HandleFunc("GET /api/v1/networks/{id}/dns-records", a.withUser(RoleViewer, a.dnsRecords))
	mux.HandleFunc("POST /api/v1/networks/{id}/dns-records", a.withUser(RoleOperator, a.dnsRecords))
	mux.HandleFunc("PUT /api/v1/networks/{id}/dns-records/{record_id}", a.withUser(RoleOperator, a.updateDNSRecord))
	mux.HandleFunc("DELETE /api/v1/networks/{id}/dns-records/{record_id}", a.withUser(RoleOperator, a.deleteDNSRecord))
	mux.HandleFunc("GET /api/v1/networks/{id}/egress", a.withUser(RoleViewer, a.networkEgress))
	mux.HandleFunc("PUT /api/v1/networks/{id}/egress", a.withUser(RoleOperator, a.networkEgress))
	mux.HandleFunc("POST /api/v1/networks/{id}/publish", a.withUser(RoleOperator, a.publish))
	mux.HandleFunc("GET /api/v1/deliveries", a.withUser(RoleViewer, a.deliveries))
	mux.HandleFunc("GET /api/v1/audit", a.withUser(RoleAdmin, a.audit))
	mux.HandleFunc("DELETE /api/v1/audit", a.withUser(RoleAdmin, a.clearAudit))
	mux.HandleFunc("GET /api/v1/settings", a.withUser(RoleViewer, a.settings))
	mux.HandleFunc("PUT /api/v1/settings", a.withUser(RoleAdmin, a.settings))
	mux.HandleFunc("GET /api/v1/settings/geoip", a.withUser(RoleViewer, a.geoIPStatus))
	mux.HandleFunc("PUT /api/v1/settings/geoip", a.withUser(RoleAdmin, a.updateGeoIP))
	mux.HandleFunc("POST /api/v1/settings/geoip/reload", a.withUser(RoleAdmin, a.reloadGeoIP))
	mux.HandleFunc("GET /api/v1/settings/geoip/lookup", a.withUser(RoleViewer, a.lookupGeoIP))
	mux.HandleFunc("GET /api/v1/settings/notifications", a.withUser(RoleViewer, a.notificationChannels))
	mux.HandleFunc("POST /api/v1/settings/notifications", a.withUser(RoleAdmin, a.notificationChannels))
	mux.HandleFunc("PUT /api/v1/settings/notifications/{id}", a.withUser(RoleAdmin, a.updateNotificationChannel))
	mux.HandleFunc("DELETE /api/v1/settings/notifications/{id}", a.withUser(RoleAdmin, a.deleteNotificationChannel))
	mux.HandleFunc("POST /api/v1/settings/notifications/{id}/test", a.withUser(RoleAdmin, a.testNotificationChannel))
	mux.HandleFunc("GET /api/v1/settings/notification-logs", a.withUser(RoleViewer, a.notificationLogs))
	mux.HandleFunc("GET /api/v1/settings/alert-rules", a.withUser(RoleViewer, a.alertRules))
	mux.HandleFunc("POST /api/v1/settings/alert-rules", a.withUser(RoleAdmin, a.alertRules))
	mux.HandleFunc("PUT /api/v1/settings/alert-rules/{id}", a.withUser(RoleAdmin, a.updateAlertRule))
	mux.HandleFunc("DELETE /api/v1/settings/alert-rules/{id}", a.withUser(RoleAdmin, a.deleteAlertRule))
	mux.HandleFunc("GET /api/v1/settings/alert-events", a.withUser(RoleViewer, a.alertEvents))
	mux.HandleFunc("DELETE /api/v1/settings/alert-events", a.withUser(RoleAdmin, a.clearAlertEvents))
	mux.HandleFunc("POST /api/v1/settings/alert-rules/{id}/evaluate", a.withUser(RoleAdmin, a.evaluateAlertRuleNow))
	mux.HandleFunc("GET /api/v1/settings/api-tokens", a.withUser(RoleAdmin, a.apiTokens))
	mux.HandleFunc("POST /api/v1/settings/api-tokens", a.withUser(RoleAdmin, a.apiTokens))
	mux.HandleFunc("DELETE /api/v1/settings/api-tokens/{id}", a.withUser(RoleAdmin, a.deleteAPIToken))
	mux.HandleFunc("GET /api/v1/settings/backup", a.withUser(RoleAdmin, a.backupDatabase))
	mux.HandleFunc("POST /api/v1/settings/backup/restore", a.withUser(RoleAdmin, a.restoreDatabase))
	mux.HandleFunc("GET /api/v1/users", a.withUser(RoleAdmin, a.users))
	mux.HandleFunc("POST /api/v1/users", a.withUser(RoleAdmin, a.users))
	mux.HandleFunc("PATCH /api/v1/users/{id}", a.withUser(RoleAdmin, a.updateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", a.withUser(RoleAdmin, a.deleteUser))
	mux.HandleFunc("GET /api/v1/agent/update", a.withUser(RoleViewer, a.agentUpdateInfo))
	mux.HandleFunc("POST /api/v1/agent/enrollment-tokens", a.withUser(RoleAdmin, a.createEnrollment))
	mux.HandleFunc("GET /agent/install.sh", a.agentInstallScript)
	mux.HandleFunc("GET /agent/uninstall.sh", a.agentUninstallScript)
	mux.HandleFunc("GET /agent/download", a.agentDownload)
	mux.HandleFunc("POST /agent/v1/enroll", a.enroll)
	mux.HandleFunc("GET /agent/v1/update", a.agentUpdate)
	mux.HandleFunc("GET /agent/v1/config", a.agentConfig)
	mux.HandleFunc("GET /agent/v1/peer-config", a.agentPeerConfig)
	mux.HandleFunc("GET /agent/v1/location", a.agentLocation)
	mux.HandleFunc("POST /agent/v1/status", a.agentStatus)
	mux.HandleFunc("POST /agent/v1/heartbeat", a.agentHeartbeat)
	mux.HandleFunc("GET /agent/v1/commands", a.agentCommands)
	mux.HandleFunc("POST /agent/v1/commands/{id}/progress", a.agentCommandProgress)
	mux.HandleFunc("POST /agent/v1/commands/{id}/result", a.agentCommandResult)
	return cors(mux)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) setupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := a.store.HasUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取初始化状态失败")
		return
	}
	status := DatabaseStatus{Configured: true, Driver: a.databaseDriver}
	if a.database != nil {
		status = a.database.Status()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized":           initialized,
		"database_configured":   status.Configured,
		"database_driver":       status.Driver,
		"database_configurable": a.database != nil,
		"setup_token_required":  a.setupToken != "",
	})
}

func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.checkSetupAllowed(ip) {
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
		return
	}
	a.recordSetupAttempt(ip)
	if !a.requireSetupToken(w, r) {
		return
	}
	if a.database != nil && !a.database.Status().Configured {
		writeError(w, http.StatusConflict, "请先配置数据库再创建管理员")
		return
	}
	var in struct {
		Email    string
		Name     string
		Password string
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	parsedEmail, err := mail.ParseAddress(in.Email)
	if err != nil || strings.ToLower(parsedEmail.Address) != in.Email {
		writeError(w, http.StatusBadRequest, "请输入有效的邮箱地址")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "名称不能为空")
		return
	}
	if len(in.Password) < 8 {
		writeError(w, http.StatusBadRequest, "密码至少需要 8 个字符")
		return
	}
	passwordHash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	user := User{
		ID:           newID("usr"),
		TenantID:     newID("tenant"),
		Email:        in.Email,
		Name:         in.Name,
		Role:         RoleAdmin,
		Active:       true,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now().UTC(),
	}
	if a.database != nil {
		err = a.database.CreateInitialAdmin(user)
	} else {
		err = a.store.CreateInitialAdmin(user)
	}
	if err != nil {
		if errors.Is(err, errAlreadyInitialized) {
			writeError(w, http.StatusConflict, "WireMesh 已完成初始化")
			return
		}
		if errors.Is(err, errDatabaseNotConfigured) {
			writeError(w, http.StatusConflict, "请先配置数据库再创建管理员")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建管理员失败")
		return
	}
	a.clearSetupAttempts(ip)
	writeJSON(w, http.StatusCreated, map[string]any{"user": publicUser(user)})
}
func (a *App) withUser(required Role, next func(http.ResponseWriter, *http.Request, claims)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := requestToken(r)
		c, err := a.auth.Parse(token)
		if err != nil {
			// 用户令牌校验失败时尝试 API 令牌（长期凭据，供脚本/CI 使用）。
			apiToken, ok := a.lookupAPIToken(token)
			if !ok {
				writeError(w, http.StatusUnauthorized, "身份验证或权限不足")
				return
			}
			_ = a.store.UpdateAPITokenLastUsed(apiToken.ID, time.Now().UTC())
			c = claims{Subject: "api_token:" + apiToken.ID, TenantID: apiToken.TenantID, Role: RoleAdmin}
		} else if a.isRevokedToken(token) {
			writeError(w, http.StatusUnauthorized, "身份验证或权限不足")
			return
		} else {
			a.touchSession(token)
		}
		if !allowed(c.Role, required) {
			writeError(w, http.StatusUnauthorized, "身份验证或权限不足")
			return
		}
		next(w, r, c)
	}
}

// requestToken 优先读取 HttpOnly cookie（浏览器），回退到 Authorization 头
// （Agent 协议、测试与非浏览器客户端）。
func requestToken(r *http.Request) string {
	if cookie, err := r.Cookie(authCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

const (
	loginMaxAttempts = 5
	loginWindow      = 15 * time.Minute
)

// clientIP 提取请求来源 IP（忽略端口），用于登录限流键。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// checkLoginAllowed 返回该邮箱+IP 组合是否允许继续尝试登录。
func (a *App) checkLoginAllowed(email, ip string) bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	key := strings.ToLower(strings.TrimSpace(email)) + "\x00" + ip
	now := time.Now()
	kept := make([]time.Time, 0, len(a.loginFailures[key]))
	for _, at := range a.loginFailures[key] {
		if now.Sub(at) <= loginWindow {
			kept = append(kept, at)
		}
	}
	a.loginFailures[key] = kept
	return len(kept) < loginMaxAttempts
}

func (a *App) recordLoginFailure(email, ip string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	key := strings.ToLower(strings.TrimSpace(email)) + "\x00" + ip
	a.loginFailures[key] = append(a.loginFailures[key], time.Now())
}

func (a *App) clearLoginFailures(email, ip string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	delete(a.loginFailures, strings.ToLower(strings.TrimSpace(email))+"\x00"+ip)
}

// ---- 初始化接口防护（setup / configureDatabase / testDatabase）----

const (
	setupMaxAttempts = 5
	setupWindow      = time.Minute
)

// requireSetupToken 校验初始化接口令牌（constant-time 比较）。
// 未配置 WIREMESH_SETUP_TOKEN 时放行（兼容单机快速体验），配置后所有初始化接口必须携带。
func (a *App) requireSetupToken(w http.ResponseWriter, r *http.Request) bool {
	if a.setupToken == "" {
		return true
	}
	if !hmac.Equal([]byte(r.Header.Get("X-Setup-Token")), []byte(a.setupToken)) {
		writeError(w, http.StatusUnauthorized, "初始化口令无效")
		return false
	}
	return true
}

// checkSetupAllowed 初始化接口按 IP 限流（5 次/分钟），未初始化窗口同样受限，
// 防止未认证探测与初始化口令爆破。
func (a *App) checkSetupAllowed(ip string) bool {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	now := time.Now()
	kept := make([]time.Time, 0, len(a.setupAttempts[ip]))
	for _, at := range a.setupAttempts[ip] {
		if now.Sub(at) <= setupWindow {
			kept = append(kept, at)
		}
	}
	a.setupAttempts[ip] = kept
	return len(kept) < setupMaxAttempts
}

func (a *App) recordSetupAttempt(ip string) {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	a.setupAttempts[ip] = append(a.setupAttempts[ip], time.Now())
}

func (a *App) clearSetupAttempts(ip string) {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	delete(a.setupAttempts, ip)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, OTP string }
	if !decode(w, r, &in) {
		return
	}
	ip := clientIP(r)
	if !a.checkLoginAllowed(in.Email, ip) {
		writeError(w, http.StatusTooManyRequests, "登录尝试次数过多，请 15 分钟后再试")
		return
	}
	token, user, err := a.auth.Login(in.Email, in.Password)
	if err != nil {
		a.recordLoginFailure(in.Email, ip)
		if errors.Is(err, errLoginPersistence) {
			writeError(w, http.StatusInternalServerError, "保存登录状态失败")
		} else {
			writeError(w, http.StatusUnauthorized, err.Error())
		}
		return
	}
	if user.TotpEnabled {
		secretBytes, decryptErr := a.box.Decrypt(user.TotpSecret)
		if decryptErr != nil || !verifyTOTP(string(secretBytes), in.OTP, time.Now()) {
			a.recordLoginFailure(in.Email, ip)
			// 区分「需要输入验证码」与「验证码错误」，前端据此展示提示
			if strings.TrimSpace(in.OTP) == "" {
				writeError(w, http.StatusUnauthorized, "otp_required")
			} else {
				writeError(w, http.StatusUnauthorized, "otp_invalid")
			}
			return
		}
	}
	a.clearLoginFailures(in.Email, ip)
	a.auditEvent(user.TenantID, user.ID, "auth.login", "user", user.ID, nil)
	a.recordSession(user, token, r.UserAgent())
	// 设置 HttpOnly + SameSite cookie，浏览器后续请求自动携带；Authorization 头仍作为非浏览器客户端的回退。
	ttl := a.auth.sessionTTL(user.TenantID)
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds())})
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": publicUser(user)})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	a.revokeCurrentSession(requestToken(r))
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

// changePassword 允许登录用户修改自己的密码（需验证旧密码）。
func (a *App) changePassword(w http.ResponseWriter, r *http.Request, c claims) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "新密码至少需要 8 个字符")
		return
	}
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account no longer exists")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.OldPassword)) != nil {
		writeError(w, http.StatusUnauthorized, "旧密码不正确")
		return
	}
	passwordHash, err := hashPassword(in.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	if err := a.store.UpdateUserPassword(user.ID, passwordHash); err != nil {
		writeError(w, http.StatusInternalServerError, "保存密码失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "auth.password.change", "user", user.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}
func (a *App) me(w http.ResponseWriter, r *http.Request, c claims) {
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		// 用户已被删除（或令牌失效）时返回 401，前端据此登出
		writeError(w, http.StatusUnauthorized, "account no longer exists")
		return
	}
	writeJSON(w, http.StatusOK, publicUser(user))
}
func publicUser(u User) map[string]any {
	var lastLoginAt any
	if !u.LastLoginAt.IsZero() {
		lastLoginAt = u.LastLoginAt
	}
	return map[string]any{"id": u.ID, "tenant_id": u.TenantID, "email": u.Email, "name": u.Name, "role": u.Role, "active": u.Active, "last_login_at": lastLoginAt}
}

func (a *App) projects(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		items, err := a.store.ListProjects(c.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取项目列表失败")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	var in struct{ Name, Description string }
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "名称不能为空")
		return
	}
	v := Project{ID: newID("prj"), TenantID: c.TenantID, Name: in.Name, Description: in.Description, CreatedAt: time.Now()}
	if err := a.store.CreateProject(v); err != nil {
		writeError(w, http.StatusInternalServerError, "创建项目失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "project.create", "project", v.ID, nil)
	writeJSON(w, 201, v)
}
func (a *App) networks(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		items, err := a.store.ListNetworks(c.TenantID, r.URL.Query().Get("project_id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取网络列表失败")
			return
		}
		writeJSON(w, 200, items)
		return
	}
	var in struct {
		ProjectID string   `json:"project_id"`
		Name      string   `json:"name"`
		CIDR      string   `json:"cidr"`
		DNS       string   `json:"dns"`
		Topology  Topology `json:"topology"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, err := a.store.GetProject(c.TenantID, in.ProjectID); err != nil {
		writeError(w, http.StatusNotFound, "项目不存在")
		return
	}
	if in.Topology == "" {
		in.Topology = TopologyFullMesh
	}
	if _, err := AllocateAddress(in.CIDR, nil); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.Topology != TopologyFullMesh && in.Topology != TopologyHubSpoke && in.Topology != TopologyCustom {
		writeError(w, 400, "网络拓扑类型无效")
		return
	}
	v := Network{ID: newID("net"), TenantID: c.TenantID, ProjectID: in.ProjectID, Name: in.Name, CIDR: in.CIDR, DNS: in.DNS, Topology: in.Topology, CreatedAt: time.Now()}
	if err := a.store.CreateNetwork(v); err != nil {
		writeError(w, http.StatusInternalServerError, "创建网络失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "network.create", "network", v.ID, nil)
	writeJSON(w, 201, v)
}
func (a *App) nodes(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		items, err := a.store.ListNodes(c.TenantID, r.URL.Query().Get("network_id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取节点列表失败")
			return
		}
		writeJSON(w, 200, items)
		return
	}
	var in struct {
		NetworkID    string            `json:"network_id"`
		Name         string            `json:"name"`
		Endpoint     string            `json:"endpoint"`
		Region       string            `json:"region"`
		OS           string            `json:"os"`
		AgentVersion string            `json:"agent_version"`
		Labels       map[string]string `json:"labels"`
	}
	if !decode(w, r, &in) {
		return
	}
	network, err := a.store.GetNetwork(c.TenantID, in.NetworkID)
	if err != nil {
		writeError(w, 404, "网络不存在")
		return
	}
	node, err := a.createNode(c.TenantID, network, in.Name, in.Endpoint, in.Region, in.OS, in.AgentVersion, in.Labels)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "node.create", "node", node.ID, nil)
	writeJSON(w, 201, node)
}

func (a *App) networkPeers(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	items, err := a.store.ListPeers(c.TenantID, network.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list network peers")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *App) addPeer(w http.ResponseWriter, r *http.Request, c claims) {
	networkID := r.PathValue("id")
	network, err := a.store.GetNetwork(c.TenantID, networkID)
	if err != nil {
		writeError(w, 404, "网络不存在")
		return
	}
	if network.Topology != TopologyCustom {
		writeError(w, 409, "手动 Peer 关系需要自定义拓扑")
		return
	}
	var in struct {
		SourceNodeID string `json:"source_node_id"`
		TargetNodeID string `json:"target_node_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	source, e1 := a.store.GetNode(c.TenantID, in.SourceNodeID)
	target, e2 := a.store.GetNode(c.TenantID, in.TargetNodeID)
	if e1 != nil || e2 != nil || source.NetworkID != networkID || target.NetworkID != networkID || source.ID == target.ID {
		writeError(w, 400, "无效的 Peer 关系")
		return
	}
	v := PeerRelation{ID: newID("peer"), TenantID: c.TenantID, NetworkID: networkID, SourceNodeID: source.ID, TargetNodeID: target.ID, CreatedAt: time.Now()}
	if err := a.store.AddPeer(v); err != nil {
		writeError(w, http.StatusInternalServerError, "创建 Peer 关系失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "peer.create", "network", networkID, nil)
	writeJSON(w, 201, v)
}

func (a *App) publish(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "网络不存在")
		return
	}
	result, err := a.publishAndAudit(c.TenantID, c.Subject, network, "config.publish", nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusCreated
	if result.Unchanged {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

// publishAndAudit publishes a network and records the outcome under the given
// action, appending ".noop" when nothing changed. It is shared by the explicit
// publish endpoint and the automatic publish triggered by node edits.
func (a *App) publishAndAudit(tenantID, actorID string, network Network, action string, extra map[string]string) (ConfigPublishResult, error) {
	result, err := a.publishNetwork(tenantID, network)
	if err != nil {
		return ConfigPublishResult{}, err
	}
	if result.Unchanged {
		action += ".noop"
	}
	metadata := map[string]string{
		"version":       fmt.Sprint(result.Version),
		"changed_nodes": fmt.Sprint(len(result.ChangedNodeIDs)),
		"offline_nodes": fmt.Sprint(len(result.OfflineNodeIDs)),
	}
	for key, value := range extra {
		metadata[key] = value
	}
	a.auditEvent(tenantID, actorID, action, "network", network.ID, metadata)
	return result, nil
}

func (a *App) publishNetwork(tenantID string, network Network) (ConfigPublishResult, error) {
	allNodes, err := a.store.ListNodes(tenantID, network.ID)
	if err != nil {
		return ConfigPublishResult{}, fmt.Errorf("list network nodes: %w", err)
	}
	nodes := make([]Node, 0, len(allNodes))
	for _, node := range allNodes {
		if node.Enabled {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return ConfigPublishResult{}, fmt.Errorf("network has no enabled nodes")
	}
	peers, err := a.store.ListPeers(tenantID, network.ID)
	if err != nil {
		return ConfigPublishResult{}, fmt.Errorf("list network peers: %w", err)
	}
	resources, err := a.store.ListAccessResources(tenantID, network.ID)
	if err != nil {
		return ConfigPublishResult{}, fmt.Errorf("list access resources: %w", err)
	}
	policies, err := a.store.ListAccessPolicies(tenantID, network.ID)
	if err != nil {
		return ConfigPublishResult{}, fmt.Errorf("list access policies: %w", err)
	}
	var egress *EgressConfig
	if egressConfig, egressErr := a.store.GetEgressConfig(tenantID, network.ID); egressErr == nil {
		egress = &egressConfig
	}
	configs, err := CompileTopology(network, nodes, peers, CompileOptions{Resources: resources, Policies: policies, Egress: egress}, a.box)
	if err != nil {
		return ConfigPublishResult{}, err
	}
	// 私钥在修订中必须加密持久化（AGENTS.md：Persist only through EncryptedSecret），
	// 与历史明文格式兼容：读取方通过 openRevisionConfig 解密。
	sealedConfigs, err := a.sealRevisionConfigs(configs)
	if err != nil {
		return ConfigPublishResult{}, fmt.Errorf("encrypt revision private keys: %w", err)
	}
	previous, previousErr := a.store.LatestRevision(tenantID, network.ID)
	if previousErr != nil && !errors.Is(previousErr, errNotFound) {
		return ConfigPublishResult{}, fmt.Errorf("read latest configuration revision: %w", previousErr)
	}
	deliveryTargets := make(map[string]bool, len(nodes))
	changedNodeIDs := make([]string, 0, len(nodes))
	queuedNodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		// 加密后的私钥是随机化产物，不能直接比较；解密旧修订后与本次明文编译结果比较
		sameConfig := false
		if !errors.Is(previousErr, errNotFound) {
			if previousConfig, openErr := a.openRevisionConfig(previous.Configs[node.ID]); openErr == nil {
				sameConfig = reflect.DeepEqual(previousConfig, configs[node.ID])
			}
		}
		if !sameConfig {
			deliveryTargets[node.ID] = true
			changedNodeIDs = append(changedNodeIDs, node.ID)
			queuedNodeIDs = append(queuedNodeIDs, node.ID)
			continue
		}
		applied, err := a.nodeHasAppliedConfigVersion(tenantID, node.ID, previous.Version)
		if err != nil {
			return ConfigPublishResult{}, fmt.Errorf("read node configuration delivery: %w", err)
		}
		if !applied {
			deliveryTargets[node.ID] = true
			queuedNodeIDs = append(queuedNodeIDs, node.ID)
		}
	}

	revision := previous
	if len(changedNodeIDs) > 0 {
		revision = ConfigRevision{ID: newID("rev"), TenantID: tenantID, ProjectID: network.ProjectID, NetworkID: network.ID, Version: previous.Version + 1, Configs: sealedConfigs, CreatedAt: time.Now()}
		if errors.Is(previousErr, errNotFound) {
			revision.Version = 1
		}
		if err := a.store.CreateRevision(revision); err != nil {
			return ConfigPublishResult{}, fmt.Errorf("create configuration revision: %w", err)
		}
	}
	if len(changedNodeIDs) == 0 && len(queuedNodeIDs) == 0 {
		return ConfigPublishResult{
			RevisionID: previous.ID, NetworkID: network.ID, Version: previous.Version,
			ChangedNodeIDs: []string{}, QueuedNodeIDs: []string{}, OfflineNodeIDs: []string{}, Unchanged: true,
		}, nil
	}
	settings, settingsErr := a.tenantSettings(tenantID)
	if settingsErr != nil {
		return ConfigPublishResult{}, fmt.Errorf("read delivery settings: %w", settingsErr)
	}
	offlineAfter := time.Duration(settings.StatusRules.AgentOfflineSec) * time.Second
	result := ConfigPublishResult{
		RevisionID: revision.ID, NetworkID: network.ID, Version: revision.Version,
		ChangedNodeIDs: changedNodeIDs, QueuedNodeIDs: make([]string, 0, len(queuedNodeIDs)), OfflineNodeIDs: []string{},
		Unchanged: len(changedNodeIDs) == 0,
	}
	commands := make([]AgentCommand, 0, len(queuedNodeIDs))
	for _, node := range nodes {
		if !deliveryTargets[node.ID] {
			continue
		}
		hasDelivery, err := a.nodeHasConfigDelivery(tenantID, node.ID, revision.Version)
		if err != nil {
			return ConfigPublishResult{}, fmt.Errorf("read node configuration delivery: %w", err)
		}
		if !hasDelivery {
			if err := a.store.CreateDelivery(ConfigDelivery{ID: newID("delivery"), TenantID: tenantID, NodeID: node.ID, Version: revision.Version, State: "pending", UpdatedAt: time.Now()}); err != nil {
				return ConfigPublishResult{}, fmt.Errorf("create configuration delivery: %w", err)
			}
		}
		commands = append(commands, newAgentCommand(tenantID, node.ID, agentCommandTypeApplyConfig))
		result.QueuedNodeIDs = append(result.QueuedNodeIDs, node.ID)
		if node.LastSeen.IsZero() || time.Since(node.LastSeen) > offlineAfter {
			result.OfflineNodeIDs = append(result.OfflineNodeIDs, node.ID)
		}
	}
	if err := a.createAgentCommandsParallel(commands); err != nil {
		return ConfigPublishResult{}, fmt.Errorf("queue configuration delivery: %w", err)
	}
	return result, nil
}

func (a *App) nodeHasAppliedConfigVersion(tenantID, nodeID string, version uint64) (bool, error) {
	deliveries, err := a.store.ListDeliveries(tenantID, nodeID)
	if err != nil {
		return false, err
	}
	for _, delivery := range deliveries {
		if delivery.Version == version && delivery.State == "applied" {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) nodeHasConfigDelivery(tenantID, nodeID string, version uint64) (bool, error) {
	deliveries, err := a.store.ListDeliveries(tenantID, nodeID)
	if err != nil {
		return false, err
	}
	for _, delivery := range deliveries {
		if delivery.Version == version {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) nodeHasOpenConfigDelivery(tenantID, nodeID string, version uint64) (bool, error) {
	deliveries, err := a.store.ListDeliveries(tenantID, nodeID)
	if err != nil {
		return false, err
	}
	for _, delivery := range deliveries {
		if delivery.Version != version {
			continue
		}
		return delivery.State == "pending" || delivery.State == "failed" || delivery.State == "rolled_back", nil
	}
	return false, nil
}
func (a *App) deliveries(w http.ResponseWriter, r *http.Request, c claims) {
	items, err := a.store.ListDeliveries(c.TenantID, r.URL.Query().Get("node_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置下发记录失败")
		return
	}
	writeJSON(w, 200, items)
}
func (a *App) audit(w http.ResponseWriter, r *http.Request, c claims) {
	limit, offset := parseLogPage(r)
	items, err := a.store.ListAuditPage(c.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审计日志失败")
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	if items == nil {
		items = []AuditEvent{}
	}
	writeJSON(w, http.StatusOK, AuditLogPage{Items: items, Limit: limit, Offset: offset, HasMore: hasMore})
}

func (a *App) clearAudit(w http.ResponseWriter, r *http.Request, c claims) {
	if err := a.store.ClearAudit(c.TenantID); err != nil {
		writeError(w, http.StatusInternalServerError, "清除审计日志失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) createEnrollment(w http.ResponseWriter, r *http.Request, c claims) {
	var in struct {
		ProjectID  string `json:"project_id"`
		NetworkID  string `json:"network_id"`
		TTLMinutes int    `json:"ttl_minutes"`
	}
	if !decode(w, r, &in) {
		return
	}
	network, err := a.store.GetNetwork(c.TenantID, in.NetworkID)
	if err != nil || network.ProjectID != in.ProjectID {
		writeError(w, 400, "网络不属于该项目")
		return
	}
	if in.TTLMinutes <= 0 || in.TTLMinutes > 1440 {
		in.TTLMinutes = 30
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	v := EnrollmentToken{ID: newID("enroll"), TenantID: c.TenantID, ProjectID: in.ProjectID, NetworkID: in.NetworkID, Token: token, ExpiresAt: time.Now().Add(time.Duration(in.TTLMinutes) * time.Minute)}
	if err := a.store.CreateEnrollment(v); err != nil {
		writeError(w, http.StatusInternalServerError, "创建接入令牌失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "agent.enrollment_token.create", "network", in.NetworkID, nil)
	writeJSON(w, 201, map[string]any{"token": token, "expires_at": v.ExpiresAt, "network_id": in.NetworkID})
}

func (a *App) enroll(w http.ResponseWriter, r *http.Request) {
	var in wireproto.EnrollmentRequest
	if !decode(w, r, &in) {
		return
	}
	enrollment, err := a.store.ConsumeEnrollment(in.Token)
	if err != nil {
		writeError(w, 401, "invalid or expired enrollment token")
		return
	}
	network, err := a.store.GetNetwork(enrollment.TenantID, enrollment.NetworkID)
	if err != nil {
		writeError(w, 404, "network not found")
		return
	}
	node, err := a.createNode(enrollment.TenantID, network, in.Name, in.Endpoint, in.Region, in.OS, in.AgentVersion, in.Labels)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	cert, key, fingerprint, expires, err := a.issueAgentCertificate(node.ID)
	if err != nil {
		writeError(w, 500, "failed to issue agent certificate")
		return
	}
	if err := a.store.CreateIdentity(AgentIdentity{NodeID: node.ID, CertificatePEM: cert, CertificateFingerprint: fingerprint, ExpiresAt: expires}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist agent identity")
		return
	}
	a.auditEvent(enrollment.TenantID, node.ID, "agent.enroll", "node", node.ID, nil)
	writeJSON(w, 201, wireproto.EnrollmentResponse{
		Node: wireproto.EnrollmentNode{ID: node.ID}, CertificatePEM: cert, PrivateKeyPEM: key,
		CertificateFingerprint: fingerprint, ExpiresAt: formatWireTime(expires), CAPEM: a.caPEM(),
	})
}

// agentNode authorizes an agent by its enrolled mTLS certificate when it reaches
// the server directly. X-Agent-ID also supports local HTTP and TLS-terminating
// reverse proxies; proxy deployments must keep the backend listener private.
func (a *App) agentNode(w http.ResponseWriter, r *http.Request) (Node, bool) {
	// 严格模式（RequireAgentClientCert）：直连 TLS 必须携带有效客户端证书，
	// 不允许仅凭 X-Agent-ID 头冒充节点。
	if a.requireAgentClientCert && (r.TLS == nil || len(r.TLS.PeerCertificates) == 0) {
		writeError(w, http.StatusUnauthorized, "agent client certificate required")
		return Node{}, false
	}
	nodeID := r.Header.Get("X-Agent-ID")
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		certificateNodeID := r.TLS.PeerCertificates[0].Subject.CommonName
		if nodeID != "" && nodeID != certificateNodeID {
			writeError(w, http.StatusUnauthorized, "agent identity mismatch")
			return Node{}, false
		}
		nodeID = certificateNodeID
	}
	if nodeID == "" {
		writeError(w, 401, "missing agent identity")
		return Node{}, false
	}
	if node, err := a.store.GetNodeByID(nodeID); err == nil {
		return node, true
	}
	writeError(w, 401, "unknown agent identity")
	return Node{}, false
}

// AgentTLSConfig verifies enrolled client certificates while allowing browsers
// to call the user-facing API on the same HTTPS listener without a certificate.
// 严格模式下（RequireAgentClientCert）则要求所有 agent 端点请求携带有效证书。
func (a *App) AgentTLSConfig() *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(a.ca)
	clientAuth := tls.VerifyClientCertIfGiven
	if a.requireAgentClientCert {
		clientAuth = tls.RequireAndVerifyClientCert
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: clientAuth, ClientCAs: pool}
}
func (a *App) agentConfig(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	if !node.Enabled {
		writeError(w, http.StatusLocked, "node is disabled")
		return
	}
	revision, err := a.store.LatestRevision(node.TenantID, node.NetworkID)
	if err != nil {
		writeError(w, 404, "no published configuration")
		return
	}
	config, ok := revision.Configs[node.ID]
	if !ok {
		writeError(w, 404, "node not included in published configuration")
		return
	}
	config, err = a.openRevisionConfig(config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt node configuration")
		return
	}
	hasOpen, err := a.nodeHasOpenConfigDelivery(node.TenantID, node.ID, revision.Version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read node configuration delivery")
		return
	}
	if !hasOpen {
		writeError(w, 404, "no pending configuration for this node")
		return
	}
	a.auditEvent(node.TenantID, node.ID, "agent.config.read", "node", node.ID, map[string]string{"version": fmt.Sprint(revision.Version)})
	writeJSON(w, 200, wireproto.ConfigResponse{Version: revision.Version, Config: config})
}
func (a *App) agentStatus(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	var in wireproto.ConfigStatusRequest
	if !decode(w, r, &in) {
		return
	}
	if in.State != "applied" && in.State != "failed" && in.State != "rolled_back" {
		writeError(w, 400, "invalid delivery state")
		return
	}
	node.LastSeen = time.Now()
	if err := a.store.UpdateNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, "保存节点状态失败")
		return
	}
	delivery := ConfigDelivery{ID: newID("delivery"), TenantID: node.TenantID, NodeID: node.ID, Version: in.Version, State: in.State, Message: in.Message, UpdatedAt: time.Now()}
	if err := a.store.UpdateDelivery(delivery); err != nil {
		writeError(w, http.StatusInternalServerError, "保存下发状态失败")
		return
	}
	a.auditEvent(node.TenantID, node.ID, "agent.config."+in.State, "node", node.ID, map[string]string{"version": fmt.Sprint(in.Version)})
	writeJSON(w, 200, map[string]string{"status": "recorded"})
}

func (a *App) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	var in wireproto.HeartbeatRequest
	if !decode(w, r, &in) {
		return
	}
	node.LastSeen = time.Now()
	if strings.TrimSpace(in.Hostname) != "" {
		node.Hostname = strings.TrimSpace(in.Hostname)
	}
	node.InterfaceSelector = strings.TrimSpace(in.Interfaces)
	node.CollectionError = strings.TrimSpace(in.CollectionError)
	if strings.TrimSpace(in.OS) != "" {
		node.OS = strings.TrimSpace(in.OS)
	}
	if strings.TrimSpace(in.AgentVersion) != "" {
		node.AgentVersion = strings.TrimSpace(in.AgentVersion)
	}
	if in.Labels != nil {
		node.Labels = in.Labels
	}
	adoptedInterface := ""
	adoptedConfiguration := false
	if in.WireGuard != nil {
		node.WireGuard = wireGuardStatusFromWire(in.WireGuard)
		a.geoLocatePeerEndpoints(node.TenantID, node.WireGuard)
		adoptedInterface, adoptedConfiguration = a.adoptReportedNodeConfiguration(&node)
	}
	if in.PeerConfigs != nil {
		node.PeerConfigFiles = sanitizeAgentPeerConfigFiles(peerConfigFilesFromWire(in.PeerConfigs), node.LastSeen)
	}
	location := geoIPLocation{}
	if in.Location != nil {
		location = *in.Location
	}
	a.applyAutomaticNodeLocation(&node, location, r)
	a.adoptPublicEndpoint(&node, location, r)
	if err := a.store.UpdateNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record agent heartbeat")
		return
	}
	if adoptedConfiguration {
		a.auditEvent(node.TenantID, node.ID, "agent.config.observed", "node", node.ID, map[string]string{
			"address": node.Address, "interface": adoptedInterface, "listen_port": fmt.Sprint(node.ListenPort), "mtu": fmt.Sprint(node.MTU),
		})
	}
	samples := make([]TrafficSample, 0, len(node.WireGuard))
	for _, iface := range node.WireGuard {
		var rx, tx int64
		for _, peer := range iface.Peers {
			rx += peer.ReceiveBytes
			tx += peer.TransmitBytes
		}
		samples = append(samples, TrafficSample{ID: newID("traffic"), TenantID: node.TenantID, NodeID: node.ID, InterfaceName: iface.Name, ReceiveBytes: rx, TransmitBytes: tx, RecordedAt: node.LastSeen})
	}
	if err := a.store.AddTrafficSamples(samples); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record traffic samples")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "recorded", "server_time": node.LastSeen})
}

func (a *App) createNode(tenantID string, network Network, name, endpoint, region, os, agentVersion string, labels map[string]string) (Node, error) {
	if strings.TrimSpace(name) == "" {
		return Node{}, errors.New("node name is required")
	}
	if labels == nil {
		labels = map[string]string{}
	}
	curve := ecdh.X25519()
	private, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return Node{}, err
	}
	privateText := base64.StdEncoding.EncodeToString(private.Bytes())
	secret, err := a.box.Encrypt([]byte(privateText))
	if err != nil {
		return Node{}, err
	}
	// 地址分配可能与并发 enroll 竞争；唯一约束冲突时重新分配重试一次。
	var node Node
	for attempt := 0; attempt < 2; attempt++ {
		existing, listErr := a.store.ListNodes(tenantID, network.ID)
		if listErr != nil {
			return Node{}, listErr
		}
		allocated := make([]string, 0, len(existing))
		for _, existingNode := range existing {
			allocated = append(allocated, existingNode.Address)
		}
		address, allocErr := AllocateAddress(network.CIDR, allocated)
		if allocErr != nil {
			return Node{}, allocErr
		}
		node = Node{ID: newID("node"), TenantID: tenantID, ProjectID: network.ProjectID, NetworkID: network.ID, Name: name, Enabled: true, ListenPort: defaultNodeListenPort, MTU: defaultNodeMTU, Address: address, Endpoint: endpoint, Region: region, OS: os, AgentVersion: agentVersion, Labels: labels, PublicKey: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), PrivateKey: secret, WireGuard: []WireGuardInterfaceStatus{}, CreatedAt: time.Now()}
		createErr := a.store.CreateNode(node)
		if createErr == nil {
			return node, nil
		}
		if !errors.Is(createErr, errAddressConflict) {
			return Node{}, createErr
		}
	}
	return Node{}, errors.New("node address allocation conflicted with a concurrent enrollment")
}

func (a *App) newCertificateAuthority() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "WireMesh Agent CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	a.ca = cert
	a.caKey = key
	return nil
}
func (a *App) issueAgentCertificate(nodeID string) (string, string, string, time.Time, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	expires := time.Now().AddDate(1, 0, 0)
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: nodeID}, NotBefore: time.Now().Add(-time.Minute), NotAfter: expires, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, a.ca, &key.PublicKey, a.caKey)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	privateDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	fingerprint := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER})), hex.EncodeToString(fingerprint[:]), expires, nil
}
func (a *App) caPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.ca.Raw}))
}

// persistedCA 是落盘的 Agent CA 表示：私钥用 master key 加密，证书明文。
type persistedCA struct {
	Version int             `json:"version"`
	CertPEM string          `json:"cert_pem"`
	Key     EncryptedSecret `json:"key_secret"`
}

// loadOrCreateCA 加载持久化的 Agent CA；文件不存在时生成并加密落盘。
// 服务重启后复用同一 CA，避免已接入 Agent 的证书全部失效。
func (a *App) loadOrCreateCA(path string) error {
	raw, err := os.ReadFile(path)
	if err == nil {
		var stored persistedCA
		if jsonErr := json.Unmarshal(raw, &stored); jsonErr != nil {
			return fmt.Errorf("load agent CA %s: %w", path, jsonErr)
		}
		der, decryptErr := a.box.Decrypt(stored.Key)
		if decryptErr != nil {
			return fmt.Errorf("decrypt agent CA %s: %w", path, decryptErr)
		}
		key, parseErr := x509.ParseECPrivateKey(der)
		if parseErr != nil {
			return fmt.Errorf("parse agent CA key %s: %w", path, parseErr)
		}
		block, _ := pem.Decode([]byte(stored.CertPEM))
		if block == nil {
			return fmt.Errorf("parse agent CA cert %s: invalid PEM", path)
		}
		cert, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return fmt.Errorf("parse agent CA cert %s: %w", path, parseErr)
		}
		a.ca = cert
		a.caKey = key
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read agent CA %s: %w", path, err)
	}
	if err := a.newCertificateAuthority(); err != nil {
		return err
	}
	return a.persistCA(path)
}

// persistCA 把当前 CA 加密写入磁盘（0600），供下次启动复用。
func (a *App) persistCA(path string) error {
	key, ok := a.caKey.(*ecdsa.PrivateKey)
	if !ok {
		return errors.New("agent CA key is not an ECDSA private key")
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	secret, err := a.box.Encrypt(der)
	if err != nil {
		return err
	}
	stored := persistedCA{Version: 1, CertPEM: a.caPEM(), Key: secret}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// sealRevisionConfigs 加密修订中每个节点的私钥（明文仅存在于内存编译阶段）。
func (a *App) sealRevisionConfigs(configs map[string]NodeConfig) (map[string]NodeConfig, error) {
	sealed := make(map[string]NodeConfig, len(configs))
	for id, config := range configs {
		secret, err := a.box.Encrypt([]byte(config.PrivateKey))
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(secret)
		if err != nil {
			return nil, err
		}
		config.PrivateKey = string(raw)
		sealed[id] = config
	}
	return sealed, nil
}

// openRevisionConfig 解出修订中节点的明文配置；兼容历史明文私钥格式（Base64 WG 密钥）。
func (a *App) openRevisionConfig(config NodeConfig) (NodeConfig, error) {
	key := config.PrivateKey
	if !strings.HasPrefix(key, "{") {
		return config, nil
	}
	var secret EncryptedSecret
	if err := json.Unmarshal([]byte(key), &secret); err != nil {
		return NodeConfig{}, fmt.Errorf("parse encrypted private key: %w", err)
	}
	plaintext, err := a.box.Decrypt(secret)
	if err != nil {
		return NodeConfig{}, fmt.Errorf("decrypt node private key: %w", err)
	}
	config.PrivateKey = string(plaintext)
	return config, nil
}

func (a *App) auditEvent(tenant, actor, action, resourceType, resourceID string, metadata map[string]string) {
	a.store.AddAudit(AuditEvent{ID: newID("audit"), TenantID: tenant, ActorID: actor, Action: action, ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now()})
}

// maxJSONBodyBytes 限制所有 JSON 端点的请求体大小，防止超大 body 造成内存 DoS。
const maxJSONBodyBytes = 1 << 20 // 1 MiB

// pageParams 解析列表端点的 limit/offset 查询参数（默认 100，上限 500）。
func pageParams(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, 400, "请求体不是有效的 JSON 或超出大小限制")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	value := reflect.ValueOf(v)
	if value.IsValid() && value.Kind() == reflect.Slice && value.IsNil() {
		v = reflect.MakeSlice(value.Type(), 0, 0).Interface()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func newID(prefix string) string {
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(randomBytes(12))
}
func randomBytes(n int) []byte {
	v := make([]byte, n)
	if _, err := rand.Read(v); err != nil {
		// crypto/rand 失败意味着无法产生安全随机数，继续使用部分填充的字节
		// 会导致 ID 与一次性令牌可被预测，属于不可恢复的安全问题。
		panic("crypto/rand read failed: " + err.Error())
	}
	return v
}
func cors(next http.Handler) http.Handler {
	allowOrigin := strings.TrimSpace(os.Getenv("WIREMESH_CORS_ORIGIN"))
	if allowOrigin == "" {
		allowOrigin = "http://localhost:5173"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Agent-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
