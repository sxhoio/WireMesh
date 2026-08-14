package control

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestDeleteUserCascadesAPITokens：删除用户后，其创建的 API 令牌立即失效。
func TestDeleteUserCascadesAPITokens(t *testing.T) {
	app := testApp(t)
	admin, adminToken := initializeTestAdmin(t, app, "s5-admin@example.com", "strong-password")

	// 创建第二个管理员（admin2），由它创建 API 令牌
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/users", adminToken, `{"name":"Admin2","email":"s5-admin2@example.com","password":"admin2-pass","role":"admin"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create admin2: %d %s", response.Code, response.Body.String())
	}
	users, _ := app.store.ListUsers(admin.TenantID)
	var admin2 User
	for _, user := range users {
		if user.Email == "s5-admin2@example.com" {
			admin2 = user
		}
	}
	login := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/login", "", `{"email":"s5-admin2@example.com","password":"admin2-pass"}`)
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil || session.Token == "" {
		t.Fatalf("admin2 login: %d %s", login.Code, login.Body.String())
	}
	create := authenticatedRequest(app, http.MethodPost, "/api/v1/settings/api-tokens", session.Token, `{"name":"ci-token","ttl_days":365}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create api token: %d %s", create.Code, create.Body.String())
	}
	var created struct {
		Token    string   `json:"token"`
		APIToken APIToken `json:"api_token"`
	}
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil || created.Token == "" {
		t.Fatalf("api token response: %s", create.Body.String())
	}
	if created.APIToken.CreatedBy != admin2.ID {
		t.Fatalf("api token must record creator, got %q want %q", created.APIToken.CreatedBy, admin2.ID)
	}
	// 令牌可用
	if code := authenticatedRequest(app, http.MethodGet, "/api/v1/users", created.Token, "").Code; code != http.StatusOK {
		t.Fatalf("api token should work: %d", code)
	}
	// 删除 admin2 → 令牌级联吊销
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/users/"+admin2.ID, adminToken, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete admin2: %d %s", response.Code, response.Body.String())
	}
	if code := authenticatedRequest(app, http.MethodGet, "/api/v1/users", created.Token, "").Code; code != http.StatusUnauthorized {
		t.Fatalf("api token must be revoked after creator deletion: %d", code)
	}
}
