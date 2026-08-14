package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentInstallScriptDefaultsFollowProtocol：缺省 mtls 参数时脚本保留
// 自动判断逻辑（USE_MTLS 为空，交由脚本按协议判断）。
func TestAgentInstallScriptDefaultsFollowProtocol(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "install-default-key"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://wiremesh.example.com/agent/install.sh", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("install script status %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `USE_MTLS=""`) {
		t.Fatalf("default script must leave USE_MTLS empty for auto-detection")
	}
}

// TestAgentInstallScriptMtlsParam：mtls=true/false 查询参数预置默认值。
func TestAgentInstallScriptMtlsParam(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "install-mtls-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ param, want string }{
		{param: "mtls=true", want: `USE_MTLS="true"`},
		{param: "mtls=false", want: `USE_MTLS="false"`},
		{param: "mtls=1", want: `USE_MTLS="true"`},
	} {
		request := httptest.NewRequest(http.MethodGet, "http://wiremesh.example.com/agent/install.sh?"+test.param, nil)
		response := httptest.NewRecorder()
		app.Router().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status %d", test.param, response.Code)
		}
		body := response.Body.String()
		if !strings.Contains(body, test.want) {
			t.Fatalf("%s: expected %q in script", test.param, test.want)
		}
	}
}

// TestAgentInstallScriptEmbedsUpdatePublicKey：配置签名密钥且要求注入时，
// 脚本内嵌 PEM 公钥；未配置时留空。
func TestAgentInstallScriptEmbedsUpdatePublicKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER}))

	app, err := NewApp(Config{MasterKey: "install-pubkey-key", UpdateSigningKey: privPEM})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://wiremesh.example.com/agent/install.sh?update_public_key=true", nil)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "BEGIN PUBLIC KEY") {
		t.Fatalf("script must embed the signing public key PEM")
	}
	if !strings.Contains(body, "WIREMESH_UPDATE_PUBLIC_KEY=") {
		t.Fatalf("script must write the public key into agent.env")
	}
	if !strings.Contains(body, "--update-public-key") {
		t.Fatalf("script must pass --update-public-key to the Agent service")
	}

	// 未配置签名密钥时，即使请求也不注入
	plain, err := NewApp(Config{MasterKey: "install-pubkey-key2"})
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	plain.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://wiremesh.example.com/agent/install.sh?update_public_key=true", nil))
	if strings.Contains(response.Body.String(), "BEGIN PUBLIC KEY") {
		t.Fatal("script must not embed a public key when no signing key is configured")
	}
}
