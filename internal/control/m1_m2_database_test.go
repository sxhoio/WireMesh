package control

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigureDatabaseGenericError：M-1——configureDatabase 失败时不回显
// 驱动级错误详情（内网探测 oracle），只返回通用文案；细节进服务端日志。
func TestConfigureDatabaseGenericError(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewDatabaseManager(filepath.Join(dir, "config.json"), "m1-key")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	app, err := NewApp(Config{MasterKey: "m1-key", AgentInsecureHTTP: true, Store: manager.Store(), Database: manager, SetupToken: "cfg-token"})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"driver":"postgres","host":"127.0.0.1","port":9,"database":"x","username":"u","password":"p","ssl_mode":"disable"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/database", strings.NewReader(body))
	request.Header.Set("X-Setup-Token", "cfg-token")
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	// 连接失败（127.0.0.1:9 拒绝）→ 通用文案，不得回显 dial/驱动细节
	if response.Code != http.StatusBadRequest {
		t.Fatalf("configure failure must 400: %d %s", response.Code, response.Body.String())
	}
	text := response.Body.String()
	if strings.Contains(text, "dial tcp") || strings.Contains(text, "connection refused") || strings.Contains(text, "host=") {
		t.Fatalf("response must not leak driver details: %s", text)
	}
	if !strings.Contains(text, "数据库连接失败") {
		t.Fatalf("response must use generic message: %s", text)
	}
}

// TestResolveDatabaseHostIPReplacesDomain：M-2——域名主机在配置校验时被
// 替换为已解析的安全 IP，连接不再重新解析（防 DNS rebinding）。
func TestResolveDatabaseHostIPReplacesDomain(t *testing.T) {
	// 用已知公网域名验证：解析后应返回 IP 字面量且通过安全校验
	resolved, ok := resolveDatabaseHostIP("one.one.one.one")
	if !ok {
		t.Skip("network resolution unavailable")
	}
	ip := net.ParseIP(resolved)
	if ip == nil {
		t.Fatalf("resolved value must be an IP literal: %q", resolved)
	}
	if isUnsafeDatabaseIP(ip) {
		t.Fatalf("resolved IP must not be unsafe: %s", resolved)
	}
	// IP 字面量与 localhost 不替换
	if value, ok := resolveDatabaseHostIP("localhost"); ok || value != "localhost" {
		t.Fatalf("localhost must not be replaced: %q %v", value, ok)
	}
	if value, ok := resolveDatabaseHostIP("8.8.8.8"); ok || value != "8.8.8.8" {
		t.Fatalf("IP literal must not be replaced: %q %v", value, ok)
	}
}
