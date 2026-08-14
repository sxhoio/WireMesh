package control

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// metrics 输出 Prometheus 文本格式的聚合指标。端点未认证，只暴露跨租户的
// 聚合计数、进程级资源使用与认证状态，不包含节点名、地址或任何租户内
// 敏感信息（可观测性专项：补充进程与安全指标）。
func (a *App) metrics(w http.ResponseWriter, r *http.Request) {
	nodes, _ := a.store.CountNodes()
	users, _ := a.store.CountUsers()
	var b strings.Builder
	b.WriteString("# HELP wiremesh_up Whether the WireMesh control plane is up.\n")
	b.WriteString("# TYPE wiremesh_up gauge\nwiremesh_up 1\n")
	b.WriteString("# HELP wiremesh_nodes_total Total number of nodes across all tenants.\n")
	b.WriteString("# TYPE wiremesh_nodes_total gauge\n")
	fmt.Fprintf(&b, "wiremesh_nodes_total %d\n", nodes)
	b.WriteString("# HELP wiremesh_users_total Total number of users across all tenants.\n")
	b.WriteString("# TYPE wiremesh_users_total gauge\n")
	fmt.Fprintf(&b, "wiremesh_users_total %d\n", users)
	b.WriteString("# HELP wiremesh_database_driver_info Database driver in use (1 = active).\n")
	b.WriteString("# TYPE wiremesh_database_driver_info gauge\n")
	fmt.Fprintf(&b, "wiremesh_database_driver_info{driver=%q} 1\n", a.databaseDriver)

	// 进程级资源（Go runtime）——Prometheus 标准进程指标之外的补充
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	b.WriteString("# HELP wiremesh_process_goroutines Current number of goroutines.\n")
	b.WriteString("# TYPE wiremesh_process_goroutines gauge\n")
	fmt.Fprintf(&b, "wiremesh_process_goroutines %d\n", runtime.NumGoroutine())
	b.WriteString("# HELP wiremesh_process_heap_bytes Current heap bytes allocated.\n")
	b.WriteString("# TYPE wiremesh_process_heap_bytes gauge\n")
	fmt.Fprintf(&b, "wiremesh_process_heap_bytes %d\n", memory.HeapAlloc)
	b.WriteString("# HELP wiremesh_process_start_time_seconds Start time of the process.\n")
	b.WriteString("# TYPE wiremesh_process_start_time_seconds gauge\n")
	fmt.Fprintf(&b, "wiremesh_process_start_time_seconds %d\n", a.startTime.Unix())

	// 认证与凭据状态（聚合，不含用户身份）
	a.sessionMu.Lock()
	sessions := len(a.sessions)
	revoked := len(a.revokedTokens)
	a.sessionMu.Unlock()
	b.WriteString("# HELP wiremesh_auth_sessions_active Current number of active sessions (memory).\n")
	b.WriteString("# TYPE wiremesh_auth_sessions_active gauge\n")
	fmt.Fprintf(&b, "wiremesh_auth_sessions_active %d\n", sessions)
	b.WriteString("# HELP wiremesh_auth_revoked_tokens Current number of revoked tokens held in memory.\n")
	b.WriteString("# TYPE wiremesh_auth_revoked_tokens gauge\n")
	fmt.Fprintf(&b, "wiremesh_auth_revoked_tokens %d\n", revoked)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
