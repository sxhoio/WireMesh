package control

import (
	"fmt"
	"net/http"
	"strings"
)

// metrics 输出 Prometheus 文本格式的聚合指标。端点未认证，只暴露跨租户的
// 聚合计数与数据库驱动名，不包含节点名、地址或任何租户内敏感信息。
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
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
