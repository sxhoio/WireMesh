package control

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestLogoutRevocationPersistsAcrossRestart：M-6——logout 的吊销记录携带
// 租户 ID，重启后 loadRevokedTokens 仍会加载，令牌不会"复活"。
func TestLogoutRevocationPersistsAcrossRestart(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "m6-logout@example.com", "strong-password")
	_ = admin

	// 验证令牌当前有效（me 返回 200）
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("token must be valid before logout: %d", response.Code)
	}

	// logout（带 token 头）
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", response.Code, response.Body.String())
	}

	// 吊销记录必须携带租户 ID（否则 loadRevokedTokens 跳过）
	hash := sessionTokenHash(token)
	rows, err := app.store.ListRevokedTokens()
	if err != nil {
		t.Fatal(err)
	}
	foundWithTenant := false
	for _, row := range rows {
		if row.TokenHash == hash && row.TenantID != "" {
			foundWithTenant = true
		}
	}
	if !foundWithTenant {
		t.Fatalf("logout revocation must carry tenant ID; rows=%#v", rows)
	}

	// 模拟重启：全新 app 从同一 store 加载吊销
	app2 := &App{store: app.store, auth: app.auth, sessions: map[string]UserSession{}, revokedTokens: map[string]time.Time{}}
	app2.loadRevokedTokens()
	if !app2.isRevokedToken(token) {
		t.Fatal("revoked token must be rejected after restart")
	}
}
