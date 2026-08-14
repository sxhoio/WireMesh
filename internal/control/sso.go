package control

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SSOConfig 是租户级的 OIDC 单点登录配置。客户端密钥加密存储，响应中只回传
// 是否已配置的布尔值。
type SSOConfig struct {
	TenantID               string          `json:"-"`
	Issuer                 string          `json:"issuer"`
	ClientID               string          `json:"client_id"`
	ClientSecret           EncryptedSecret `json:"-"`
	ClientSecretConfigured bool            `json:"client_secret_configured"`
	Enabled                bool            `json:"enabled"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type ssoState struct {
	TenantID    string
	Issuer      string
	ClientID    string
	Secret      string
	Nonce       string
	Verifier    string
	RedirectURI string
	ExpiresAt   time.Time
}

const ssoStateTTL = 10 * time.Minute

// maxSSOStates 是内存 SSO state 表上限（S11）：未完成的授权流程在 TTL 内
// 最多占用有限条目，防攻击者批量发起登录放大内存。
const maxSSOStates = 5000

func (a *App) ssoConfig(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		config, err := a.store.GetSSOConfig(c.TenantID)
		if err != nil {
			writeJSON(w, http.StatusOK, SSOConfig{Enabled: false})
			return
		}
		config.ClientSecretConfigured = config.ClientSecret.Ciphertext != ""
		writeJSON(w, http.StatusOK, config)
		return
	}
	var in struct {
		Issuer   string `json:"issuer"`
		ClientID string `json:"client_id"`
		Secret   string `json:"client_secret"`
		Enabled  bool   `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Issuer = strings.TrimRight(strings.TrimSpace(in.Issuer), "/")
	if in.Enabled {
		if in.Issuer == "" || !strings.HasPrefix(in.Issuer, "http") {
			writeError(w, http.StatusBadRequest, "issuer must be a valid URL")
			return
		}
		// S7：issuer 必须是 http/https 完整 URL，拒绝 javascript: 等非 HTTP 协议
		parsed, parseErr := url.Parse(in.Issuer)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			writeError(w, http.StatusBadRequest, "issuer must be a valid HTTP(S) URL")
			return
		}
		if in.ClientID == "" {
			writeError(w, http.StatusBadRequest, "client_id is required")
			return
		}
	}
	config := SSOConfig{TenantID: c.TenantID, Issuer: in.Issuer, ClientID: in.ClientID, Enabled: in.Enabled, UpdatedAt: time.Now().UTC()}
	if in.Secret != "" {
		encrypted, err := a.box.Encrypt([]byte(in.Secret))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to protect client secret")
			return
		}
		config.ClientSecret = encrypted
	}
	if err := a.store.UpsertSSOConfig(config); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save SSO configuration")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "settings.sso.update", "tenant", c.TenantID, nil)
	config.ClientSecretConfigured = config.ClientSecret.Ciphertext != ""
	writeJSON(w, http.StatusOK, config)
}

func (a *App) ssoLogin(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant")
	configs, err := a.store.AllSSOConfigs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list SSO configurations")
		return
	}
	enabled := make([]SSOConfig, 0, len(configs))
	for _, config := range configs {
		if config.Enabled {
			enabled = append(enabled, config)
		}
	}
	if tenant == "" {
		if len(enabled) == 1 {
			tenant = enabled[0].TenantID
		} else {
			ids := make([]string, 0, len(enabled))
			for _, config := range enabled {
				ids = append(ids, config.TenantID)
			}
			writeJSON(w, http.StatusOK, map[string]any{"tenants": ids})
			return
		}
	}
	var config SSOConfig
	found := false
	for _, candidate := range enabled {
		if candidate.TenantID == tenant {
			config, found = candidate, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusConflict, "SSO is not configured for this tenant")
		return
	}
	discovery, err := fetchOIDCDiscovery(r.Context(), config.Issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, "SSO provider discovery failed")
		return
	}
	state := base64.RawURLEncoding.EncodeToString(randomBytes(24))
	nonce := base64.RawURLEncoding.EncodeToString(randomBytes(16))
	// H-3（P1）：PKCE——生成 64 字节 code_verifier，S256 摘要作为
	// code_challenge 随授权请求发出；即使授权码被截获，攻击者没有
	// verifier 也无法向 token 端点兑换。
	verifier := base64.RawURLEncoding.EncodeToString(randomBytes(64))
	verifierHash := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(verifierHash[:])
	secret, err := a.box.Decrypt(config.ClientSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SSO client secret unavailable")
		return
	}
	redirectURI, err := a.ssoRedirectURI(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的 SSO 回调地址")
		return
	}
	a.ssoMu.Lock()
	// S11：容量上限——先清理过期 state，仍超限则删除最旧的。
	now := time.Now()
	for key, info := range a.ssoStates {
		if now.After(info.ExpiresAt) {
			delete(a.ssoStates, key)
		}
	}
	for len(a.ssoStates) >= maxSSOStates {
		var oldestKey string
		var oldestAt time.Time
		first := true
		for key, info := range a.ssoStates {
			if first || info.ExpiresAt.Before(oldestAt) {
				oldestKey, oldestAt, first = key, info.ExpiresAt, false
			}
		}
		if first {
			break
		}
		delete(a.ssoStates, oldestKey)
	}
	a.ssoStates[state] = ssoState{TenantID: tenant, Issuer: config.Issuer, ClientID: config.ClientID, Secret: string(secret), Nonce: nonce, Verifier: verifier, RedirectURI: redirectURI, ExpiresAt: now.Add(ssoStateTTL)}
	a.ssoMu.Unlock()
	authURL := discovery.AuthorizationEndpoint + "?response_type=code&client_id=" + url.QueryEscape(config.ClientID) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) + "&scope=" + url.QueryEscape("openid email profile") + "&state=" + url.QueryEscape(state) +
		"&nonce=" + url.QueryEscape(nonce) + "&code_challenge=" + url.QueryEscape(codeChallenge) + "&code_challenge_method=S256"
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

func (a *App) ssoCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state")
		return
	}
	a.ssoMu.Lock()
	stateInfo, ok := a.ssoStates[state]
	delete(a.ssoStates, state)
	a.ssoMu.Unlock()
	if !ok || time.Now().After(stateInfo.ExpiresAt) {
		writeError(w, http.StatusBadRequest, "SSO state expired or invalid")
		return
	}
	// S7：回调的 redirect_uri 必须与发起登录时一致（绑定 state），
	// 防止攻击者篡改 Host 头把授权码重定向到自己的域名。
	redirectURI, err := a.ssoRedirectURI(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的 SSO 回调地址")
		return
	}
	if redirectURI != stateInfo.RedirectURI {
		writeError(w, http.StatusBadRequest, "SSO callback host mismatch")
		return
	}
	discovery, err := fetchOIDCDiscovery(r.Context(), stateInfo.Issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, "SSO provider discovery failed")
		return
	}
	accessToken, idToken, err := exchangeOIDCCode(r.Context(), discovery.TokenEndpoint, stateInfo.ClientID, stateInfo.Secret, code, redirectURI, stateInfo.Verifier)
	if err != nil {
		writeError(w, http.StatusBadGateway, "SSO token exchange failed")
		return
	}
	// 校验 ID token：签名（JWKS）、issuer、audience、过期时间与 nonce
	if err := verifyOIDCIDToken(r.Context(), idToken, discovery.JWKSURI, stateInfo.Issuer, stateInfo.ClientID, stateInfo.Nonce); err != nil {
		writeError(w, http.StatusUnauthorized, "SSO ID token validation failed")
		return
	}
	email, err := fetchOIDCUserEmail(r.Context(), discovery.UserinfoEndpoint, accessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, "SSO userinfo failed")
		return
	}
	user, err := a.store.GetUserByEmail(email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "SSO account has no matching WireMesh user")
		return
	}
	if !user.Active {
		writeError(w, http.StatusUnauthorized, "SSO account is disabled")
		return
	}
	// 回调必须回到发起登录的同一租户，防止跨租户会话签发
	if user.TenantID != stateInfo.TenantID {
		writeError(w, http.StatusUnauthorized, "SSO account does not belong to this tenant")
		return
	}
	ttl := a.auth.sessionTTL(user.TenantID)
	sessionToken := a.auth.issueTTL(user, ttl)
	_ = a.store.UpdateUserLastLogin(user.ID, time.Now().UTC())
	a.recordSession(user, sessionToken, r.UserAgent())
	a.auditEvent(user.TenantID, user.ID, "auth.login.sso", "user", user.ID, nil)
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: sessionToken, Path: "/", HttpOnly: true, Secure: a.cookieSecure(r), SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds())})
	http.Redirect(w, r, "/", http.StatusFound)
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

func fetchOIDCDiscovery(ctx context.Context, issuer string) (oidcDiscovery, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcDiscovery{}, err
	}
	response, err := oidcHTTPClient().Do(request)
	if err != nil {
		return oidcDiscovery{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return oidcDiscovery{}, fmt.Errorf("discovery returned %s", response.Status)
	}
	var discovery oidcDiscovery
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&discovery); err != nil {
		return oidcDiscovery{}, err
	}
	if discovery.AuthorizationEndpoint == "" || discovery.TokenEndpoint == "" || discovery.UserinfoEndpoint == "" {
		return oidcDiscovery{}, errors.New("incomplete OIDC discovery document")
	}
	return discovery, nil
}

func exchangeOIDCCode(ctx context.Context, tokenEndpoint, clientID, clientSecret, code, redirectURI, codeVerifier string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	// H-3（P1）：PKCE——兑换时携带 code_verifier，IdP 校验其与授权请求的
	// code_challenge 匹配；截获的授权码无 verifier 无法兑换。
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := oidcHTTPClient().Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("token endpoint returned %s", response.Status)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", "", err
	}
	if payload.AccessToken == "" {
		return "", "", errors.New("token response missing access_token")
	}
	if payload.IDToken == "" {
		return "", "", errors.New("token response missing id_token")
	}
	return payload.AccessToken, payload.IDToken, nil
}

func fetchOIDCUserEmail(ctx context.Context, userinfoEndpoint, accessToken string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoEndpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := oidcHTTPClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo endpoint returned %s", response.Status)
	}
	var payload struct {
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	if payload.EmailVerified != nil && !*payload.EmailVerified {
		return "", errors.New("userinfo email is not verified by the provider")
	}
	email := strings.ToLower(strings.TrimSpace(payload.Email))
	if email == "" {
		return "", errors.New("userinfo response missing email")
	}
	return email, nil
}

// ssoRedirectURI 构造 SSO 回调地址。配置了 WIREMESH_PUBLIC_URL 时使用该
// 固定公网源（H-3：不再信任攻击者可控制的 Host 头）；未配置时回退到
// 校验过的 Host 头（S7 保留，配合 PKCE 兜底）。
func (a *App) ssoRedirectURI(r *http.Request) (string, error) {
	if a.publicURL != "" {
		parsed, err := url.Parse(a.publicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return "", errors.New("invalid configured public URL")
		}
		return strings.TrimRight(a.publicURL, "/") + "/api/v1/auth/sso/callback", nil
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "", errors.New("missing Host header")
	}
	if err := validateRedirectHost(host); err != nil {
		return "", err
	}
	return scheme + "://" + host + "/api/v1/auth/sso/callback", nil
}

// validateRedirectHost 校验 SSO redirect_uri 的 Host 部分只允许 host[:port]。
func validateRedirectHost(host string) error {
	if strings.ContainsAny(host, "/\\?#@ \t\r\n\"'<>") {
		return errors.New("invalid redirect host")
	}
	hostname := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		hostname = parsed
	} else if strings.Count(host, ":") > 1 {
		// 可能是无端口的 IPv6 字面量
		hostname = strings.Trim(host, "[]")
	}
	if hostname == "" {
		return errors.New("invalid redirect host")
	}
	for _, char := range hostname {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '-' || char == ':' || char == '[' || char == ']' {
			continue
		}
		return errors.New("invalid redirect host")
	}
	return nil
}
