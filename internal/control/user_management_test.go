package control

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestUserManagementGuardsAndDisable(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "user-admin@example.com", "strong-password")

	// 创建两个用户：operator + viewer
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/users", token, `{"name":"Operator","email":"op@example.com","password":"operator-pass","role":"operator"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create operator: %d %s", response.Code, response.Body.String())
	}
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/users", token, `{"name":"Viewer","email":"viewer@example.com","password":"viewer-pass","role":"viewer"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create viewer: %d %s", response.Code, response.Body.String())
	}
	users, _ := app.store.ListUsers(admin.TenantID)
	var operator, viewer User
	for _, user := range users {
		switch user.Email {
		case "op@example.com":
			operator = user
		case "viewer@example.com":
			viewer = user
		}
	}
	if operator.ID == "" || viewer.ID == "" {
		t.Fatalf("users not found: %#v", users)
	}
	if !operator.Active || !viewer.Active {
		t.Fatalf("new users must be active: %#v %#v", operator, viewer)
	}

	// 升级 viewer → operator，改名
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+viewer.ID, token, `{"role":"operator","name":"Viewer2"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"role":"operator"`) {
		t.Fatalf("update user: %d %s", response.Code, response.Body.String())
	}
	// 停用 operator → 其登录被拒绝
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+operator.ID, token, `{"active":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("disable user: %d %s", response.Code, response.Body.String())
	}
	login := authenticatedRequest(app, http.MethodPost, "/api/v1/auth/login", "", `{"email":"op@example.com","password":"operator-pass"}`)
	if login.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user must not login: %d %s", login.Code, login.Body.String())
	}
	// 重新启用后可登录
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+operator.ID, token, `{"active":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("enable user: %d %s", response.Code, response.Body.String())
	}
	login = authenticatedRequest(app, http.MethodPost, "/api/v1/auth/login", "", `{"email":"op@example.com","password":"operator-pass"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("enabled user must login: %d %s", login.Code, login.Body.String())
	}

	// 不能修改自己
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+admin.ID, token, `{"role":"viewer"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("self update must be rejected: %d", response.Code)
	}
	// 最后一个管理员不能被停用/删除
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+admin.ID, token, `{"active":false}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("self disable must be rejected: %d", response.Code)
	}
	// 再建一个管理员后，原管理员可以被停用
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/users", token, `{"name":"Admin2","email":"admin2@example.com","password":"admin2-pass","role":"admin"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create second admin: %d %s", response.Code, response.Body.String())
	}
	users, _ = app.store.ListUsers(admin.TenantID)
	var admin2 User
	for _, user := range users {
		if user.Email == "admin2@example.com" {
			admin2 = user
		}
	}
	// 第二个管理员被停用后，第一个管理员仍不能被删除（只剩自己一个 active admin）
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+admin2.ID, token, `{"active":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("disable admin2: %d %s", response.Code, response.Body.String())
	}
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/users/"+admin2.ID, token, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete admin2: %d %s", response.Code, response.Body.String())
	}

	// 删除用户：不能删除自己
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/users/"+admin.ID, token, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("self delete must be rejected: %d", response.Code)
	}
	// 删除普通用户
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/users/"+viewer.ID, token, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete viewer: %d %s", response.Code, response.Body.String())
	}
	if _, err := app.store.GetUser(viewer.ID); err == nil {
		t.Fatal("deleted user still exists")
	}
	// 跨租户不可见：另一租户管理员无法更新本租户用户 → 404
	other := User{ID: "other-admin", TenantID: "other-tenant", Email: "other-admin@example.com", Name: "Other", Role: RoleAdmin, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPatch, "/api/v1/users/"+operator.ID, app.auth.issue(other), `{"role":"viewer"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant update must 404: %d", response.Code)
	}
}
