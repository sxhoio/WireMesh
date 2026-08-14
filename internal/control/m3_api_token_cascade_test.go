package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDeactivatedOrDowngradedAdminLosesAPITokens：M-3——停用或降级管理员后，
// 其创建的 API 令牌必须级联删除（API 令牌恒为 admin 权限，否则成为后门）。
func TestDeactivatedOrDowngradedAdminLosesAPITokens(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "m3-admin@example.com", "strong-password")

	// 创建第二个管理员并为其签发 API 令牌
	other := User{ID: "m3-other", TenantID: admin.TenantID, Email: "m3-other@example.com", Name: "Other", Role: RoleAdmin, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	otherToken := app.auth.issue(other)
	createBody := `{"name":"other-api","ttl_days":30}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/api-tokens", strings.NewReader(createBody))
	request.Header.Set("Authorization", "Bearer "+otherToken)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create api token: %d %s", response.Code, response.Body.String())
	}
	tokens, err := app.store.ListAPITokens(admin.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 API token, got %d", len(tokens))
	}

	// 降级 other 为 viewer → API 令牌应被删除
	updateBody := `{"role":"viewer"}`
	request = httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+other.ID, strings.NewReader(updateBody))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("downgrade: %d %s", response.Code, response.Body.String())
	}
	tokens, err = app.store.ListAPITokens(admin.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("downgraded admin's API tokens must be deleted, got %d", len(tokens))
	}
}

// TestDeactivatedAdminLosesAPITokens：停用管理员后其 API 令牌同样失效。
func TestDeactivatedAdminLosesAPITokens(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "m3-deactivate@example.com", "strong-password")
	other := User{ID: "m3-deact-other", TenantID: admin.TenantID, Email: "m3-deact@example.com", Name: "Other", Role: RoleAdmin, Active: true, PasswordHash: "unused", CreatedAt: time.Now()}
	if err := app.store.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	otherToken := app.auth.issue(other)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/api-tokens", strings.NewReader(`{"name":"api-2","ttl_days":30}`))
	request.Header.Set("Authorization", "Bearer "+otherToken)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create api token: %d", response.Code)
	}
	// 停用 other
	request = httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+other.ID, strings.NewReader(`{"active":false}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", response.Code, response.Body.String())
	}
	tokens, err := app.store.ListAPITokens(admin.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("deactivated admin's API tokens must be deleted, got %d", len(tokens))
	}
}
