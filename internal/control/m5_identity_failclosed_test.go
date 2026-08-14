package control

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAgentCertRejectedWhenIdentityMissing：M-5——持 CA 证书但身份记录缺失
// 时 agent 端点 fail-closed（此前 errNotFound 会跳过指纹校验放行）。
func TestAgentCertRejectedWhenIdentityMissing(t *testing.T) {
	app := testApp(t)
	admin, _ := initializeTestAdmin(t, app, "m5-cert@example.com", "strong-password")
	network := Network{ID: "net-m5", TenantID: admin.TenantID, ProjectID: "p-m5", Name: "M5", CIDR: "10.95.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(Project{ID: "p-m5", TenantID: admin.TenantID, Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(admin.TenantID, network, "m5-node", "", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 签发证书但故意不登记身份（模拟身份表缺失）
	certPEM, _, _, _, err := app.issueAgentCertificate(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("invalid certificate PEM")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	// 构造带 TLS 客户端证书的请求（身份缺失 → fail-closed）
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/config", nil)
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{parsed}}
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("identity-missing agent must be rejected 401: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "not registered") && !strings.Contains(response.Body.String(), "revoked") {
		t.Fatalf("unexpected rejection message: %s", response.Body.String())
	}
}
