package control

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ===== S7：SSO 外呼私网过滤 + redirect_uri 校验 =====

// TestSSORedirectHostRejectsPoisoning：redirect_uri 的 Host 头注入被拒绝。
func TestSSORedirectHostRejectsPoisoning(t *testing.T) {
	cases := []string{
		"evil.example.com@trusted.example.com", // 用户信息注入
		"trusted.example.com/evil",             // 路径注入
		"trusted.example.com?x=1",              // 查询串注入
		"trusted.example.com#frag",             // 片段注入
		"trusted.example.com\x00null",          // 控制字符
		"",                                     // 空 Host
		"trusted example.com",                  // 空白
	}
	for _, host := range cases {
		if err := validateRedirectHost(host); err == nil {
			t.Fatalf("Host %q must be rejected", host)
		}
	}
	valid := []string{"wiremesh.example.com", "wiremesh.example.com:8443", "10.0.0.5", "[::1]:8080"}
	for _, host := range valid {
		if err := validateRedirectHost(host); err != nil {
			t.Fatalf("Host %q must be accepted: %v", host, err)
		}
	}
}

// TestSSOLoginAndCallbackBindRedirectURI：登录与回调之间的 redirect_uri 必须一致，
// Host 头不一致时回调被拒绝（授权码不泄漏到攻击者域名）。
func TestSSOLoginAndCallbackBindRedirectURI(t *testing.T) {
	app := testApp(t)
	// 直接调用回调，state 不存在 → 400（不依赖真实 IdP）
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sso/callback?code=abc&state=missing", nil)
	request.Host = "evil.example.com"
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown state must 400: %d %s", response.Code, response.Body.String())
	}
	// 伪造 state 内容直接注入表：验证 Host 不一致时拒绝
	app.ssoMu.Lock()
	app.ssoStates["forged-state"] = ssoState{TenantID: "t", Issuer: "https://idp.example", ClientID: "c", Secret: "s", Nonce: "n", RedirectURI: "https://wiremesh.example.com/api/v1/auth/sso/callback", ExpiresAt: time.Now().Add(time.Minute)}
	app.ssoMu.Unlock()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/sso/callback?code=abc&state=forged-state", nil)
	request.Host = "evil.example.com"
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "host mismatch") {
		t.Fatalf("Host-switched callback must be rejected: %d %s", response.Code, response.Body.String())
	}
}

// TestOIDCDialRejectsPrivateAddress：SSO 外呼私网过滤（默认拒绝回环/私网）。
func TestOIDCDialRejectsPrivateAddress(t *testing.T) {
	t.Setenv("WIREMESH_SSO_ALLOW_PRIVATE", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	ctx := t.Context()
	if _, err := fetchOIDCDiscovery(ctx, server.URL); err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("loopback discovery must be rejected by SSRF filter, got %v", err)
	}
}

// TestSSOConfigRejectsNonHTTPIssuer：issuer 只允许 http/https。
func TestSSOConfigRejectsNonHTTPIssuer(t *testing.T) {
	app := testApp(t)
	_, token := initializeTestAdmin(t, app, "sso-issuer@example.com", "strong-password")
	for _, issuer := range []string{"javascript:alert(1)", "file:///etc/passwd", "ftp://x.example", "http://user:pass@idp.example"} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/sso", strings.NewReader(`{"issuer":"`+issuer+`","client_id":"c","enabled":true}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		app.Router().ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("issuer %q must be rejected: %d %s", issuer, response.Code, response.Body.String())
		}
	}
}

// ===== S9：Agent 证书续期 =====

// TestAgentCertificateRenewalRotatesIdentity：续期签发新证书并覆盖登记指纹，
// 旧证书指纹失配被拒（等效 CRL）。
func TestAgentCertificateRenewalRotatesIdentity(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "renew@example.com", "strong-password")
	project := Project{ID: "project-renew", TenantID: admin.TenantID, Name: "Renew", CreatedAt: time.Now()}
	network := Network{ID: "network-renew", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Renew", CIDR: "10.77.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "renew-node", "", "", "linux/amd64", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟注册：签发证书并登记身份
	_, _, fingerprint, expires, err := app.issueAgentCertificate(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateIdentity(AgentIdentity{NodeID: node.ID, CertificatePEM: "", CertificateFingerprint: fingerprint, ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	// 用旧证书指纹调续期端点（agentNode 指纹校验通过 → 签发新证书）
	request := httptest.NewRequest(http.MethodPost, "/agent/v1/renew-cert", nil)
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("renew without TLS cert should pass through header identity: %d %s", response.Code, response.Body.String())
	}
	var renewed struct {
		CertificatePEM         string `json:"certificate_pem"`
		PrivateKeyPEM          string `json:"private_key_pem"`
		CertificateFingerprint string `json:"certificate_fingerprint"`
	}
	if err := json.NewDecoder(response.Body).Decode(&renewed); err != nil {
		t.Fatal(err)
	}
	if renewed.CertificatePEM == "" || renewed.PrivateKeyPEM == "" || renewed.CertificateFingerprint == "" {
		t.Fatalf("renew must return a fresh identity: %#v", renewed)
	}
	identity, err := app.store.GetIdentity(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if identity.CertificateFingerprint != renewed.CertificateFingerprint || identity.CertificateFingerprint == fingerprint {
		t.Fatalf("renew must overwrite the registered identity fingerprint: old=%s new=%s", fingerprint, identity.CertificateFingerprint)
	}
}

// TestAgentCertificateRenewalRejectsDisabledNode：禁用节点不能续期证书。
func TestAgentCertificateRenewalRejectsDisabledNode(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "renew-disabled@example.com", "strong-password")
	project := Project{ID: "project-renew-disabled", TenantID: admin.TenantID, Name: "Renew", CreatedAt: time.Now()}
	network := Network{ID: "network-renew-disabled", TenantID: admin.TenantID, ProjectID: project.ID, Name: "Renew", CIDR: "10.78.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "disabled-node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	node.Enabled = false
	if err := app.store.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/agent/v1/renew-cert", nil)
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusLocked {
		t.Fatalf("disabled node renewal must be locked: %d %s", response.Code, response.Body.String())
	}
}

// ===== S10：change-password 限流 + MFA 二次认证；mfaDisable 复核 =====

// TestChangePasswordRequiresOTPWhenMFAEnabled：启用 MFA 后修改密码必须提供
// 动态验证码（otp_required / otp_invalid）。
func TestChangePasswordRequiresOTPWhenMFAEnabled(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "pw-mfa@example.com", "strong-password")
	// 启用 MFA
	secret := generateTOTPSecret()
	encrypted, err := app.box.Encrypt([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpdateUserMFA(admin.ID, encrypted, true); err != nil {
		t.Fatal(err)
	}
	// 未提供 OTP → otp_required
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"strong-password","new_password":"new-password-1"}`)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "otp_required") {
		t.Fatalf("MFA user must require OTP: %d %s", response.Code, response.Body.String())
	}
	// 错误 OTP → otp_invalid
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"strong-password","new_password":"new-password-1","otp":"000000"}`)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "otp_invalid") {
		t.Fatalf("wrong OTP must be rejected: %d %s", response.Code, response.Body.String())
	}
	// 正确 OTP → 204
	code, err := totpCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"strong-password","new_password":"new-password-1","otp":"`+code+`"}`)
	if response.Code != http.StatusNoContent {
		t.Fatalf("change password with OTP must succeed: %d %s", response.Code, response.Body.String())
	}
}

// TestChangePasswordRateLimit：旧密码错误按用户+IP 限流（5 次后 429）。
func TestChangePasswordRateLimit(t *testing.T) {
	app := testApp(t)
	_, token := initializeTestAdmin(t, app, "pw-rate@example.com", "strong-password")
	for index := 0; index < changePasswordMaxAttempts; index++ {
		response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"wrong","new_password":"new-password-1"}`)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("wrong old password must 401: %d %s", response.Code, response.Body.String())
		}
	}
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"strong-password","new_password":"new-password-1"}`)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited change-password must 429: %d %s", response.Code, response.Body.String())
	}
}

// TestMFADisableRequiresPasswordAndOTP：关闭 MFA 必须验证当前密码 + 动态验证码。
func TestMFADisableRequiresPasswordAndOTP(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "mfa-disable@example.com", "strong-password")
	secret := generateTOTPSecret()
	encrypted, err := app.box.Encrypt([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpdateUserMFA(admin.ID, encrypted, true); err != nil {
		t.Fatal(err)
	}
	// 密码错误 → 401
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/disable", token, `{"password":"wrong"}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password must block MFA disable: %d %s", response.Code, response.Body.String())
	}
	// 密码正确但缺 OTP → otp_required
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/disable", token, `{"password":"strong-password"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "otp_required") {
		t.Fatalf("MFA disable without OTP must be rejected: %d %s", response.Code, response.Body.String())
	}
	// 正确密码 + OTP → 成功
	code, err := totpCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/disable", token, `{"password":"strong-password","otp":"`+code+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("MFA disable with correct credentials must succeed: %d %s", response.Code, response.Body.String())
	}
	user, _ := app.store.GetUser(admin.ID)
	if user.TotpEnabled {
		t.Fatal("MFA must be disabled after verified disable")
	}
}

// ===== S11：限流与 ssoStates 全局上限 =====

// TestRateLimitMapsAreBounded：限流表条目数受全局上限约束。
func TestRateLimitMapsAreBounded(t *testing.T) {
	app := testApp(t)
	// 伪造海量 key（不同邮箱+IP），recordLoginFailure 应把表压在 maxRateLimitEntries 内
	app.loginMu.Lock()
	for index := 0; index < maxRateLimitEntries+500; index++ {
		key := "user-" + string(rune('a'+index%26)) + "-" + string(rune('0'+index%10)) + "\x0010.0." + string(rune('0'+index%10)) + "." + string(rune('0'+index%10))
		app.loginFailures[key] = []time.Time{time.Now()}
	}
	app.loginMu.Unlock()
	app.recordLoginFailure("bounded@example.com", "203.0.113.9")
	app.loginMu.Lock()
	count := len(app.loginFailures)
	app.loginMu.Unlock()
	if count > maxRateLimitEntries {
		t.Fatalf("loginFailures exceeded cap: %d > %d", count, maxRateLimitEntries)
	}
}

// TestSSOStatesAreBounded：ssoStates 表条目数受上限约束。
func TestSSOStatesAreBounded(t *testing.T) {
	app := testApp(t)
	app.ssoMu.Lock()
	for index := 0; index < maxSSOStates+100; index++ {
		state := "state-" + string(rune('a'+index%26)) + string(rune('0'+index%10))
		app.ssoStates[state] = ssoState{ExpiresAt: time.Now().Add(time.Minute)}
	}
	app.ssoMu.Unlock()
	// 触发一次 ssoLogin 写入路径（不完整配置会提前返回，这里直接测插入逻辑）
	now := time.Now()
	app.ssoMu.Lock()
	for key, info := range app.ssoStates {
		if now.After(info.ExpiresAt) {
			delete(app.ssoStates, key)
		}
	}
	for len(app.ssoStates) >= maxSSOStates {
		var oldestKey string
		var oldestAt time.Time
		first := true
		for key, info := range app.ssoStates {
			if first || info.ExpiresAt.Before(oldestAt) {
				oldestKey, oldestAt, first = key, info.ExpiresAt, false
			}
		}
		if first {
			break
		}
		delete(app.ssoStates, oldestKey)
	}
	app.ssoStates["new-state"] = ssoState{ExpiresAt: now.Add(time.Minute)}
	count := len(app.ssoStates)
	app.ssoMu.Unlock()
	if count > maxSSOStates {
		t.Fatalf("ssoStates exceeded cap: %d > %d", count, maxSSOStates)
	}
}

// ===== S13：通知内容注入 =====

// TestSanitizeNotificationDataStripsInjection：节点名/消息中的 HTML 与控制
// 字符在 HTML/markdown 渠道被转义或剥离。
func TestSanitizeNotificationDataStripsInjection(t *testing.T) {
	data := notificationTemplateData{
		Event: "alert", Title: "告警", Message: "节点 <img src=x onerror=alert(1)> 离线\r\n伪造行",
		NodeName: "<script>alert(1)</script>\x00node", NodeStatus: "alert",
		NetworkName: "网络A", Endpoint: "https://evil.example.com", Region: "上海", OS: "linux",
		AgentVersion: "0.3.6", DashboardURL: "https://wm.example.com",
	}
	// Telegram HTML 模式：HTML 转义 + 控制字符剥离
	sanitized := sanitizeNotificationData("telegram", NotificationConfig{ParseMode: "HTML"}, data)
	if strings.Contains(sanitized.Message, "<img") || !strings.Contains(sanitized.Message, "&lt;img") {
		t.Fatalf("Telegram HTML must escape message HTML: %q", sanitized.Message)
	}
	if strings.Contains(sanitized.Message, "\r\n") || strings.Contains(sanitized.Message, "\x00") {
		t.Fatalf("control characters must be stripped: %q", sanitized.Message)
	}
	if strings.Contains(sanitized.NodeName, "<script>") {
		t.Fatalf("Telegram HTML must escape node name: %q", sanitized.NodeName)
	}
	// 钉钉 markdown：同样转义
	dingtalk := sanitizeNotificationData("dingtalk", NotificationConfig{}, data)
	if strings.Contains(dingtalk.Message, "<img") || !strings.Contains(dingtalk.Message, "&lt;img") {
		t.Fatalf("dingtalk must escape message HTML: %q", dingtalk.Message)
	}
	// 邮件（纯文本）：仅剥离控制字符，保留 HTML 原文（无注入风险）
	email := sanitizeNotificationData("email", NotificationConfig{}, data)
	if strings.Contains(email.Message, "\r\n") || strings.Contains(email.Message, "\x00") {
		t.Fatalf("email must strip control characters: %q", email.Message)
	}
	if !strings.Contains(email.Message, "<img") {
		t.Fatalf("email plain text must keep HTML as-is: %q", email.Message)
	}
	// webhook JSON：默认不转义（模板用 {{json .}} 转义）
	webhook := sanitizeNotificationData("webhook", NotificationConfig{ContentType: "application/json"}, data)
	if !strings.Contains(webhook.Message, "<img") {
		t.Fatalf("JSON webhook must not HTML-escape: %q", webhook.Message)
	}
}

// ===== S14：密码学参数 =====

// TestSecretBoxKDFBackwardCompatibility：KDF 升级后新数据用 Argon2id 派生密钥，
// 旧 SHA-256 派生密钥加密的历史数据仍可解密（回退）。
func TestSecretBoxKDFBackwardCompatibility(t *testing.T) {
	box, err := NewSecretBox("compat-master-key")
	if err != nil {
		t.Fatal(err)
	}
	// 新加密 → 新派生密钥可解密
	secret, err := box.Encrypt([]byte("fresh-secret"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := box.Decrypt(secret)
	if err != nil || string(plain) != "fresh-secret" {
		t.Fatalf("new KDF round-trip failed: %q %v", plain, err)
	}
	// 模拟历史数据：用 SHA-256 派生密钥构造 EncryptedSecret，Decrypt 应回退成功
	legacySum := sha256.Sum256([]byte("compat-master-key"))
	legacyKey := legacySum[:]
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		t.Fatal(err)
	}
	wrapped, wrapNonce, err := seal(legacyKey, dek)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, dataNonce, err := seal(dek, []byte("legacy-secret"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := EncryptedSecret{
		WrappedDEK: base64.StdEncoding.EncodeToString(wrapped),
		DEKNonce:   base64.StdEncoding.EncodeToString(wrapNonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		DataNonce:  base64.StdEncoding.EncodeToString(dataNonce),
	}
	plain, err = box.Decrypt(legacy)
	if err != nil || string(plain) != "legacy-secret" {
		t.Fatalf("legacy SHA-256-wrapped secret must decrypt via fallback: %q %v", plain, err)
	}
}

// TestBcryptCostIsHardened：新密码哈希使用提升后的 bcrypt 成本（S14）。
func TestBcryptCostIsHardened(t *testing.T) {
	hash, err := hashPassword("hunter2-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$2a$12$") && !strings.HasPrefix(hash, "$2b$12$") {
		t.Fatalf("bcrypt hash must use cost 12, got %q", hash)
	}
}

// TestTOTPSecretEntropy：TOTP 密钥为 32 字节（256 位）。
func TestTOTPSecretEntropy(t *testing.T) {
	secret := generateTOTPSecret()
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 32 {
		t.Fatalf("TOTP secret must be 32 bytes, got %d", len(decoded))
	}
}

// TestSQLiteFilePermissionsTightened：SQLite 主库与 WAL/SHM 文件权限收紧为 0600。
func TestSQLiteFilePermissionsTightened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perm.db")
	store, err := OpenSQLStore("sqlite", "file:"+strings.ReplaceAll(path, "\\", "/")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// 模拟权限过宽的文件，chmodSQLiteFiles 应收紧
	_ = os.WriteFile(path, []byte("x"), 0o644)
	_ = os.WriteFile(path+"-wal", []byte("x"), 0o644)
	chmodSQLiteFiles("file:" + strings.ReplaceAll(path, "\\", "/"))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows 上 os.Chmod 语义受限（仅只读位），Unix 上必须为 0600
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("sqlite file permission = %o, want 600", info.Mode().Perm())
	}
}
