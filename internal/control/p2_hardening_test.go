package control

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChangePassword(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "pw-admin@example.com", "old-password")

	// 旧密码错误 → 401
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"wrong-password","new_password":"new-password"}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong old password must 401: %d %s", response.Code, response.Body.String())
	}
	// 新密码过短 → 400
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"old-password","new_password":"short"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("short new password must 400: %d", response.Code)
	}
	// 正确修改 → 204
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"old-password","new_password":"new-password"}`)
	if response.Code != http.StatusNoContent {
		t.Fatalf("change password: %d %s", response.Code, response.Body.String())
	}
	// 旧密码失效、新密码可登录
	login := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/login", "", `{"email":"pw-admin@example.com","password":"old-password"}`)
	if login.Code != http.StatusUnauthorized {
		t.Fatalf("old password must not login: %d", login.Code)
	}
	login = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/login", "", `{"email":"pw-admin@example.com","password":"new-password"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("new password must login: %d %s", login.Code, login.Body.String())
	}
	_ = admin
}

func TestMeReturnsUnauthorizedForDeletedUser(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "me-admin@example.com", "strong-password")
	viewer := User{ID: "me-viewer", TenantID: admin.TenantID, Email: "me-viewer@example.com", Name: "Viewer", Role: RoleViewer, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(viewer); err != nil {
		t.Fatal(err)
	}
	viewerToken := app.auth.issue(viewer)
	if err := app.store.DeleteUser(admin.TenantID, viewer.ID); err != nil {
		t.Fatal(err)
	}
	response := authenticatedRequest(app, http.MethodGet, "/api/v1/auth/me", viewerToken, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("me must 401 for deleted user: %d %s", response.Code, response.Body.String())
	}
	// 正常用户仍 200
	response = authenticatedRequest(app, http.MethodGet, "/api/v1/auth/me", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("me must 200 for existing user: %d", response.Code)
	}
}

func TestTOTPRFCVectors(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // RFC 6238 附录 B 密钥
	at := time.Unix(59, 0).UTC()
	if !verifyTOTP(secret, "287082", at) {
		t.Fatal("RFC 6238 T=59 six-digit code must verify")
	}
	if verifyTOTP(secret, "000000", at) {
		t.Fatal("wrong code must not verify")
	}
	// 时钟漂移 ±1 窗口
	if !verifyTOTP(secret, "287082", at.Add(30*time.Second)) {
		t.Fatal("±1 time window drift must be tolerated")
	}
}

func TestOIDCIDTokenVerification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	// 本地 JWKS 服务
	jwks := map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}}
	jwksRaw, _ := json.Marshal(jwks)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksRaw)
	}))
	defer server.Close()

	sign := func(claims map[string]any, header map[string]any) string {
		headerRaw, _ := json.Marshal(header)
		claimsRaw, _ := json.Marshal(claims)
		signingInput := base64.RawURLEncoding.EncodeToString(headerRaw) + "." + base64.RawURLEncoding.EncodeToString(claimsRaw)
		sum := sha256.Sum256([]byte(signingInput))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, 5, sum[:]) // crypto.SHA256 == 5
		if err != nil {
			t.Fatal(err)
		}
		return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
	}

	validClaims := func() map[string]any {
		return map[string]any{"iss": "https://issuer.example", "aud": "client-123", "exp": time.Now().Add(time.Hour).Unix(), "nonce": "nonce-abc"}
	}
	header := map[string]any{"alg": "RS256", "kid": "test-key", "typ": "JWT"}

	// 合法 token
	token := sign(validClaims(), header)
	if err := verifyOIDCIDToken(t.Context(), token, server.URL, "https://issuer.example", "client-123", "nonce-abc"); err != nil {
		t.Fatalf("valid token must verify: %v", err)
	}
	// 签名被篡改
	tampered := token[:len(token)-4] + "AAAA"
	if err := verifyOIDCIDToken(t.Context(), tampered, server.URL, "https://issuer.example", "client-123", "nonce-abc"); err == nil {
		t.Fatal("tampered token must fail")
	}
	// issuer 不匹配
	claims := validClaims()
	claims["iss"] = "https://evil.example"
	if err := verifyOIDCIDToken(t.Context(), sign(claims, header), server.URL, "https://issuer.example", "client-123", "nonce-abc"); err == nil {
		t.Fatal("issuer mismatch must fail")
	}
	// audience 不匹配
	claims = validClaims()
	claims["aud"] = "other-client"
	if err := verifyOIDCIDToken(t.Context(), sign(claims, header), server.URL, "https://issuer.example", "client-123", "nonce-abc"); err == nil {
		t.Fatal("audience mismatch must fail")
	}
	// 过期
	claims = validClaims()
	claims["exp"] = time.Now().Add(-time.Minute).Unix()
	if err := verifyOIDCIDToken(t.Context(), sign(claims, header), server.URL, "https://issuer.example", "client-123", "nonce-abc"); err == nil {
		t.Fatal("expired token must fail")
	}
	// nonce 不匹配
	claims = validClaims()
	claims["nonce"] = "wrong-nonce"
	if err := verifyOIDCIDToken(t.Context(), sign(claims, header), server.URL, "https://issuer.example", "client-123", "nonce-abc"); err == nil {
		t.Fatal("nonce mismatch must fail")
	}
	// 非 JWT 输入
	if err := verifyOIDCIDToken(t.Context(), "not-a-jwt", server.URL, "https://issuer.example", "client-123", "nonce-abc"); err == nil {
		t.Fatal("malformed token must fail")
	}
}
