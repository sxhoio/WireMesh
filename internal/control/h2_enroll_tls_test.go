package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEnrollFailsClosedOnPlainHTTP：H-2——纯 HTTP（未显式开启开发开关）
// 时 enroll 必须被拒绝，与其余 Agent 端点一致（防注册令牌/私钥被 MITM
// 窃取）。此前 enroll 是唯一缺少 TLS 守卫的 Agent 端点。
func TestEnrollFailsClosedOnPlainHTTP(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "h2-enroll-key"})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"token":"any","name":"x","os":"linux/amd64","agent_version":"0.3.7","labels":{}}`
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", strings.NewReader(body)))
	if response.Code != http.StatusForbidden {
		t.Fatalf("plain HTTP enroll must be forbidden: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "agent endpoints require TLS") {
		t.Fatalf("enroll must explain TLS requirement: %s", response.Body.String())
	}

	// 显式开发开关放行（但不创建有效令牌时走到令牌校验）
	dev, err := NewApp(Config{MasterKey: "h2-enroll-key-dev", AgentInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	dev.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", strings.NewReader(body)))
	if response.Code == http.StatusForbidden {
		t.Fatalf("dev mode enroll must not be forbidden: %d", response.Code)
	}
}
