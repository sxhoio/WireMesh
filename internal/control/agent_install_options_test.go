package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
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

// TestAgentInstallScriptMtlsParam：mtls=true/false 查询参数预置默认值，
// 且不会产生嵌套引号（USE_MTLS="'true'" 是双重引号 bug）。
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
		if strings.Contains(body, `"'"`) {
			t.Fatalf("%s: script must not contain nested quotes", test.param)
		}
	}
}

// TestAgentInstallScriptEmbedsUpdatePublicKey：配置签名密钥且要求注入时，
// 脚本以 base64 单行携带公钥并解码写文件；未配置时留空。
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
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustMarshalPKIX(t, &key.PublicKey)}))

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
	wantB64 := base64.StdEncoding.EncodeToString([]byte(pubPEM))
	if !strings.Contains(body, wantB64) {
		t.Fatal("script must carry the signing public key as base64")
	}
	if !strings.Contains(body, "WIREMESH_UPDATE_PUBLIC_KEY_B64=") {
		t.Fatal("script must write the base64 public key into agent.env")
	}
	if !strings.Contains(body, "--update-public-key-file=/etc/wiremesh-agent/update-public-key.pem") {
		t.Fatal("script must pass --update-public-key-file to the Agent service")
	}
	if !strings.Contains(body, "base64 -d > /etc/wiremesh-agent/update-public-key.pem") {
		t.Fatal("script must decode the public key into a file")
	}

	// 未配置签名密钥时，即使请求也不注入（base64 变量留空，脚本条件分支不写入）
	plain, err := NewApp(Config{MasterKey: "install-pubkey-key2"})
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	plain.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://wiremesh.example.com/agent/install.sh?update_public_key=true", nil))
	plainBody := response.Body.String()
	if !strings.Contains(plainBody, `UPDATE_PUBLIC_KEY_B64=""`) {
		t.Fatalf("script must leave the public key empty when no signing key is configured")
	}
}

func mustMarshalPKIX(t *testing.T, key *ecdsa.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}
