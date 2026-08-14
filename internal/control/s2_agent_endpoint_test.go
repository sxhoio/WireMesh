package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentEndpointsFailClosedOnPlainHTTP：纯 HTTP 模式（未显式开启开发开关）下
// Agent 端点默认拒绝，X-Agent-ID 无法再冒充节点。
func TestAgentEndpointsFailClosedOnPlainHTTP(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "s2-closed-key"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	request.Header.Set("X-Agent-ID", "any-node")
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "agent endpoints require TLS") {
		t.Fatalf("plain HTTP agent endpoint must be rejected: %d %s", response.Code, response.Body.String())
	}
	// 心跳同样被拒绝
	request = httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(`{}`))
	request.Header.Set("X-Agent-ID", "any-node")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("plain HTTP heartbeat must be rejected: %d", response.Code)
	}
}

// TestAgentInsecureHTTPExplicitOptIn：显式开启开发开关后，HTTP 模式可用（返回
// 认证相关错误而非 403 拒绝）。
func TestAgentInsecureHTTPExplicitOptIn(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "s2-dev-key", AgentInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	// 未知节点返回 401（身份认证路径生效），而不是 403（端点整体拒绝）
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	request.Header.Set("X-Agent-ID", "unknown-node")
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("dev mode should authenticate (401 unknown node), got %d %s", response.Code, response.Body.String())
	}
}

// TestAgentTrustProxyAllowsPlainHTTP：显式配置可信反向代理时，HTTP 模式放行
// X-Agent-ID（由代理注入并保持后端私有）。M-4：仅私网/回环来源（可信反代）
// 可携带身份头；公网直连即使伪造头也拒绝。
func TestAgentTrustProxyAllowsPlainHTTP(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "s2-proxy-key", TrustProxyAgentID: true})
	if err != nil {
		t.Fatal(err)
	}
	// 可信反代（私网来源）携带 X-Agent-ID → 走到身份认证（401 unknown node）
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	request.RemoteAddr = "10.0.0.5:12345"
	request.Header.Set("X-Agent-ID", "unknown-node")
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("trust-proxy mode should authenticate (401 unknown node), got %d %s", response.Code, response.Body.String())
	}

	// M-4：公网直连携带 X-Agent-ID → 拒绝（防后端暴露被冒充）
	request = httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	request.RemoteAddr = "198.51.100.9:12345"
	request.Header.Set("X-Agent-ID", "any-node")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("public direct connection with identity header must be rejected: %d %s", response.Code, response.Body.String())
	}
}
