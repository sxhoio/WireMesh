package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSOLoginUsesConfiguredPublicURL：H-3——配置 WIREMESH_PUBLIC_URL 后，
// redirect_uri 使用固定公网源，攻击者伪造 Host 头不再影响回调地址。
func TestSSOLoginUsesConfiguredPublicURL(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "h3-sso-key", PublicURL: "https://wiremesh.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// 伪造 Host 发起 SSO 登录，redirect_uri 必须来自 PublicURL 而非 Host
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sso/login?tenant=t", nil)
	request.Host = "evil.com"
	// 需要已启用 SSO 的租户；直接调用 ssoRedirectURI 验证固定源优先
	redirectURI, err := app.ssoRedirectURI(request)
	if err != nil {
		t.Fatal(err)
	}
	if redirectURI != "https://wiremesh.example.com/api/v1/auth/sso/callback" {
		t.Fatalf("redirect_uri must use configured public URL, got %q", redirectURI)
	}
	if strings.Contains(redirectURI, "evil.com") {
		t.Fatalf("attacker Host must not influence redirect_uri: %q", redirectURI)
	}
}

// TestSSORedirectURIFallbackValidatesHost：未配置 PublicURL 时，Host 头
// 仍被严格校验（S7 保留），恶意 Host 被拒绝。
func TestSSORedirectURIFallbackValidatesHost(t *testing.T) {
	app, err := NewApp(Config{MasterKey: "h3-sso-key2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"evil.com@trusted.com", "trusted.com/path", ""} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = host
		if _, err := app.ssoRedirectURI(request); err == nil {
			t.Fatalf("Host %q must be rejected without PublicURL", host)
		}
	}
}
