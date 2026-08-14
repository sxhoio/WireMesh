package control

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

func jsonUnmarshal(data []byte, target any) error {
	return json.Unmarshal(data, target)
}

// TestDisabledUserTokenRejectedImmediately：停用用户后，其已有令牌立即失效。
func TestDisabledUserTokenRejectedImmediately(t *testing.T) {
	app := testApp(t)
	admin, adminToken := initializeTestAdmin(t, app, "s3-admin@example.com", "strong-password")

	response := authenticatedRequest(app, http.MethodPost, "/api/v1/users", adminToken, `{"name":"Op","email":"s3-op@example.com","password":"operator-pass","role":"operator"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", response.Code, response.Body.String())
	}
	users, _ := app.store.ListUsers(admin.TenantID)
	var operator User
	for _, user := range users {
		if user.Email == "s3-op@example.com" {
			operator = user
		}
	}
	operatorToken := app.auth.issue(operator)

	// 启用时可访问
	if code := authenticatedRequest(app, http.MethodGet, "/api/v1/projects", operatorToken, "").Code; code != http.StatusOK {
		t.Fatalf("active user should access: %d", code)
	}
	// 停用后立即 401
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+operator.ID, adminToken, `{"active":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", response.Code, response.Body.String())
	}
	if code := authenticatedRequest(app, http.MethodGet, "/api/v1/projects", operatorToken, "").Code; code != http.StatusUnauthorized {
		t.Fatalf("disabled user token must be rejected immediately: %d", code)
	}
}

// TestRoleDowngradeTakesEffectImmediately：降级后旧令牌按新角色执行，无需重新登录。
func TestRoleDowngradeTakesEffectImmediately(t *testing.T) {
	app := testApp(t)
	admin, adminToken := initializeTestAdmin(t, app, "s3-role@example.com", "strong-password")

	response := authenticatedRequest(app, http.MethodPost, "/api/v1/users", adminToken, `{"name":"Admin2","email":"s3-admin2@example.com","password":"admin2-pass","role":"admin"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create admin: %d %s", response.Code, response.Body.String())
	}
	users, _ := app.store.ListUsers(admin.TenantID)
	var admin2 User
	for _, user := range users {
		if user.Email == "s3-admin2@example.com" {
			admin2 = user
		}
	}
	admin2Token := app.auth.issue(admin2)
	if code := authenticatedRequest(app, http.MethodGet, "/api/v1/users", admin2Token, "").Code; code != http.StatusOK {
		t.Fatalf("admin2 should list users: %d", code)
	}
	// 降级为 viewer → 旧令牌立即失去 admin 权限（list users 需 RoleAdmin）
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+admin2.ID, adminToken, `{"role":"viewer"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("downgrade: %d %s", response.Code, response.Body.String())
	}
	if code := authenticatedRequest(app, http.MethodGet, "/api/v1/users", admin2Token, "").Code; code != http.StatusUnauthorized {
		t.Fatalf("downgraded token must lose admin rights immediately: %d", code)
	}
}

// TestRevokedTokenPersistsAcrossRestart：吊销记录持久化，重启后已吊销令牌仍失效，
// 未吊销令牌仍有效（无需重新登录）。
func TestRevokedTokenPersistsAcrossRestart(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "wiremesh.db")) + "?_pragma=foreign_keys(1)"
	store, err := OpenSQLStore("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "s3-persist-key", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	admin, adminToken := initializeTestAdmin(t, app, "s3-persist@example.com", "strong-password")

	response := authenticatedRequest(app, http.MethodPost, "/api/v1/users", adminToken, `{"name":"Op2","email":"s3-op2@example.com","password":"operator-pass","role":"operator"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", response.Code, response.Body.String())
	}
	users, _ := app.store.ListUsers(admin.TenantID)
	var operator User
	for _, user := range users {
		if user.Email == "s3-op2@example.com" {
			operator = user
		}
	}
	// 通过真实登录获得会话令牌（登录路径会 recordSession，吊销才能按用户匹配）
	login := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/login", "", `{"email":"s3-op2@example.com","password":"operator-pass"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("operator login: %d %s", login.Code, login.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := jsonUnmarshal(login.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("login response: %s", login.Body.String())
	}
	operatorToken := session.Token

	// 吊销操作员的会话（模拟强制下线）
	app.revokeUserSessions(admin.TenantID, operator.ID)
	if code := authenticatedRequest(app, http.MethodGet, "/api/v1/projects", operatorToken, "").Code; code != http.StatusUnauthorized {
		t.Fatalf("revoked token must be rejected: %d", code)
	}

	// 模拟重启：同一 store 挂到新 App，吊销记录必须仍然生效
	app2, err := NewApp(Config{MasterKey: "s3-persist-key", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if code := authenticatedRequest(app2, http.MethodGet, "/api/v1/projects", operatorToken, "").Code; code != http.StatusUnauthorized {
		t.Fatalf("revoked token must stay revoked across restart: %d", code)
	}
	// 管理员自己的令牌未被吊销，重启后仍可用
	if code := authenticatedRequest(app2, http.MethodGet, "/api/v1/projects", adminToken, "").Code; code != http.StatusOK {
		t.Fatalf("non-revoked token must survive restart: %d", code)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}
