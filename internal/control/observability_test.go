package control

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRedactCredentials：日志脱敏——URL 风格与键值风格的凭据都被遮蔽。
func TestRedactCredentials(t *testing.T) {
	cases := []struct{ input, mustNotContain string }{
		{input: `failed to connect to postgres://admin:s3cret@db.internal:5432/wiremesh`, mustNotContain: "s3cret"},
		{input: `invalid DSN: tcp(user:pass@host:3306)/db?password=topsecret`, mustNotContain: "topsecret"},
		{input: `dial error: password= hunter2 auth failed`, mustNotContain: "hunter2"},
		{input: `connect failed: pwd=supersecret`, mustNotContain: "supersecret"},
	}
	for _, test := range cases {
		redacted := RedactCredentials(test.input)
		if strings.Contains(redacted, test.mustNotContain) {
			t.Fatalf("credential leaked: input=%q redacted=%q", test.input, redacted)
		}
		if !strings.Contains(redacted, "***") {
			t.Fatalf("expected redaction marker: %q", redacted)
		}
	}
	// 明文不含凭据时原样保留
	plain := "connection refused on 127.0.0.1:9"
	if RedactCredentials(plain) != plain {
		t.Fatalf("plain text must be unchanged: %q", RedactCredentials(plain))
	}
}

// TestMetricsEndpointExposesAggregatesOnly：metrics 端点只暴露聚合/进程
// 指标，不包含节点名、地址或租户信息。
func TestMetricsEndpointExposesAggregatesOnly(t *testing.T) {
	app := testApp(t)
	_, token := initializeTestAdmin(t, app, "metrics@example.com", "strong-password")
	_ = token
	request := httptest.NewRequest("GET", "/metrics", nil)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("metrics status %d", response.Code)
	}
	body := response.Body.String()
	for _, metric := range []string{
		"wiremesh_up 1",
		"wiremesh_nodes_total",
		"wiremesh_users_total",
		"wiremesh_database_driver_info",
		"wiremesh_process_goroutines",
		"wiremesh_process_heap_bytes",
		"wiremesh_process_start_time_seconds",
		"wiremesh_auth_sessions_active",
		"wiremesh_auth_revoked_tokens",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("metrics missing %s:\n%s", metric, body)
		}
	}
	if strings.Contains(body, "metrics@example.com") {
		t.Fatalf("metrics must not expose user email:\n%s", body)
	}
	// 值行（非 HELP 注释）不得携带租户/节点标识
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "wiremesh_") && strings.Contains(strings.Join(fields[1:], " "), "tenant") {
			t.Fatalf("metrics value line must not expose tenant info: %s", line)
		}
	}
}
