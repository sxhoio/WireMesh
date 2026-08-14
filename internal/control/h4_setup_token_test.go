package control

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGenerateSetupTokenEntropy：H-4——自动生成的初始化口令为 256 位随机。
func TestGenerateSetupTokenEntropy(t *testing.T) {
	token := GenerateSetupToken()
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 32 {
		t.Fatalf("setup token must be 32 bytes, got %d", len(decoded))
	}
	if token == GenerateSetupToken() {
		t.Fatal("setup tokens must be unique")
	}
}

// TestRequireSetupTokenRejectsWrongToken：配置口令后，初始化接口必须携带
// 正确口令（constant-time），错误/缺失均 401。
func TestRequireSetupTokenRejectsWrongToken(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "h4-setup-key", SetupToken: "configured-token"})
	if err != nil {
		t.Fatal(err)
	}
	// 无口令
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("setup without token must 401: %d %s", response.Code, response.Body.String())
	}
	// 错误口令
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{}`))
	request.Header.Set("X-Setup-Token", "wrong")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("setup with wrong token must 401: %d", response.Code)
	}
	// 正确口令 → 进入业务校验（非 401）
	request = httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{}`))
	request.Header.Set("X-Setup-Token", "configured-token")
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code == http.StatusUnauthorized {
		t.Fatalf("setup with correct token must not 401")
	}
}
