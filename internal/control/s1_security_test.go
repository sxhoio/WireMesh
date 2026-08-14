package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetupTokenProtectsSetupEndpoints(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "setup-token-key", SetupToken: "secret-init-token"})
	if err != nil {
		t.Fatal(err)
	}
	// 缺少口令 → 401
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/setup", "", `{"email":"a@example.com","name":"A","password":"password"}`)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "初始化口令") {
		t.Fatalf("missing setup token must 401: %d %s", response.Code, response.Body.String())
	}
	// 错误口令 → 401
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{"email":"a@example.com","name":"A","password":"password"}`))
	request.Header.Set("X-Setup-Token", "wrong-token")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong setup token must 401: %d %s", response.Code, response.Body.String())
	}
	// 正确口令 → 创建管理员成功
	request = httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{"email":"a@example.com","name":"A","password":"password"}`))
	request.Header.Set("X-Setup-Token", "secret-init-token")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("correct setup token must create admin: %d %s", response.Code, response.Body.String())
	}
	// setupStatus 暴露 setup_token_required
	response = authenticatedRequest(app, http.MethodGet, "/api/v1/setup/status", "", "")
	if !strings.Contains(response.Body.String(), `"setup_token_required":true`) {
		t.Fatalf("setup status must report token required: %s", response.Body.String())
	}
}

func TestSetupEndpointsRateLimitedPerIP(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewDatabaseManager(dir+"/config.json", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "setup-limit-key", Store: manager.Store(), Database: manager})
	if err != nil {
		t.Fatal(err)
	}
	// 回环地址快速拒绝连接（127.0.0.1:9 通常无监听），每次失败计入限流
	payload := `{"driver":"postgres","host":"127.0.0.1","port":9,"database":"x","username":"u","password":"p","ssl_mode":"disable"}`
	for i := 0; i < setupMaxAttempts; i++ {
		response := authenticatedRequest(app, http.MethodPost, "/api/v1/setup/database/test", "", payload)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d should fail to connect: %d %s", i+1, response.Code, response.Body.String())
		}
	}
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/setup/database/test", "", payload)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt must be rate limited: %d %s", response.Code, response.Body.String())
	}
}

func TestValidateDatabaseHostRejectsPrivateAddresses(t *testing.T) {
	allowed := []string{"localhost", "127.0.0.1", "::1", "8.8.8.8", "1.1.1.1"}
	for _, host := range allowed {
		if err := validateDatabaseHost(host); err != nil {
			t.Fatalf("host %q must be allowed, got %v", host, err)
		}
	}
	blocked := []string{"10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "192.0.2.1", "0.0.0.0", "224.0.0.1", "fe80::1", "fd00::1"}
	for _, host := range blocked {
		if err := validateDatabaseHost(host); err == nil {
			t.Fatalf("host %q must be rejected", host)
		}
	}
}

func TestSetupDatabaseTestErrorDesensitized(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewDatabaseManager(dir+"/config.json", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{MasterKey: "setup-error-key", Store: manager.Store(), Database: manager})
	if err != nil {
		t.Fatal(err)
	}
	// 公网不可达地址：连接失败，但响应不得泄露驱动细节（如 connection refused）
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/setup/database/test", "", `{"driver":"postgres","host":"8.8.8.8","port":9,"database":"x","username":"u","password":"p","ssl_mode":"disable"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unreachable db must 400: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "refused") || strings.Contains(response.Body.String(), "no such host") || strings.Contains(response.Body.String(), "i/o timeout") {
		t.Fatalf("response must not leak driver details: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "数据库连接失败") {
		t.Fatalf("response should carry generic message: %s", response.Body.String())
	}
}
