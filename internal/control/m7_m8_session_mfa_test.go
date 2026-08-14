package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestMFASetupRequiresPassword：M-7——mfaSetup 需当前密码复核，
// 会话劫持者无法零验证轮换 MFA 秘密。
func TestMFASetupRequiresPassword(t *testing.T) {
	app := testApp(t)
	_, token := initializeTestAdmin(t, app, "m7-mfa@example.com", "strong-password")
	// 无密码 → 401
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/setup", token, `{}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("mfa setup without password must 401: %d %s", response.Code, response.Body.String())
	}
	// 错误密码 → 401
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/setup", token, `{"password":"wrong"}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("mfa setup with wrong password must 401: %d", response.Code)
	}
	// 正确密码 → 200 返回 secret
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/setup", token, `{"password":"strong-password"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"secret"`) {
		t.Fatalf("mfa setup with password must succeed: %d %s", response.Code, response.Body.String())
	}
}

// TestMFAEnableRequiresPassword：M-7——mfaEnable 需当前密码 + 新 OTP。
func TestMFAEnableRequiresPassword(t *testing.T) {
	app := testApp(t)
	_, token := initializeTestAdmin(t, app, "m7-enable@example.com", "strong-password")
	// 先 setup（带密码）拿到 secret
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/setup", token, `{"password":"strong-password"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", response.Code, response.Body.String())
	}
	var setup struct {
		Secret string `json:"secret"`
	}
	if err := jsonUnmarshal(response.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	code, err := totpCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// 启用但缺密码 → 401
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/enable", token, `{"otp":"`+code+`"}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("mfa enable without password must 401: %d %s", response.Code, response.Body.String())
	}
	// 密码 + OTP → 200
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/mfa/enable", token, `{"password":"strong-password","otp":"`+code+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("mfa enable with password+otp must succeed: %d %s", response.Code, response.Body.String())
	}
}

// TestChangePasswordRevokesOtherSessions：M-8——改密后其它会话令牌失效，
// 当前会话保留。
func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "m8-change@example.com", "strong-password")
	// 手动签发一个不同 TTL 的独立令牌模拟另一设备（同一秒登录会产生相同 token）
	user := mustUser(t, app, "m8-change@example.com")
	otherToken := app.auth.issueTTL(user, 30*time.Minute)
	app.recordSession(user, otherToken, "other-device")
	if otherToken == token {
		t.Fatal("other session token must differ from current")
	}
	// 改密（带当前令牌）
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/change-password", token, `{"old_password":"strong-password","new_password":"new-pass-1234"}`)
	if response.Code != http.StatusNoContent {
		t.Fatalf("change password: %d %s", response.Code, response.Body.String())
	}
	// 当前令牌仍有效
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("current session must remain valid after password change: %d %s", response.Code, response.Body.String())
	}
	// 其它令牌被吊销
	if !app.isRevokedToken(otherToken) {
		t.Fatal("other session token must be revoked after password change")
	}
	_ = admin
}
