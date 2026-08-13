package control

import (
	"fmt"
	"net/http"
	"strings"
)

// ClientConfigResponse 是导出给客户端设备（手机/桌面等非 Agent 设备）导入的
// 完整 WireGuard 配置文件。Content 为标准 INI 格式，可直接保存为 .conf 或
// 编码成二维码扫码导入。
type ClientConfigResponse struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Content string `json:"content"`
}

// nodeClientConfig 返回某个节点视角的完整 WireGuard 配置（[Interface] + [Peers]），
// 供客户端设备导入。配置来自已发布的版本，未发布时返回 404。
func (a *App) nodeClientConfig(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	revision, err := a.store.LatestRevision(node.TenantID, node.NetworkID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no published configuration")
		return
	}
	config, ok := revision.Configs[node.ID]
	if !ok {
		writeError(w, http.StatusNotFound, "node not included in published configuration")
		return
	}
	network, err := a.store.GetNetwork(node.TenantID, node.NetworkID)
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	writeJSON(w, http.StatusOK, ClientConfigResponse{
		NodeID:  node.ID,
		Name:    node.Name,
		Address: config.Address,
		Content: renderClientConfigINI(network, config),
	})
}

// renderClientConfigINI 把已发布配置渲染为 wg-quick 兼容的完整配置文件。
func renderClientConfigINI(network Network, config NodeConfig) string {
	var b strings.Builder
	b.WriteString("[Interface]\n")
	b.WriteString("PrivateKey = " + config.PrivateKey + "\n")
	b.WriteString("Address = " + config.Address + "/32\n")
	b.WriteString("ListenPort = " + fmt.Sprint(config.ListenPort) + "\n")
	b.WriteString("MTU = " + fmt.Sprint(config.MTU) + "\n")
	if dns := strings.TrimSpace(network.DNS); dns != "" {
		b.WriteString("DNS = " + dns + "\n")
	}
	b.WriteString("\n")
	for _, peer := range config.Peers {
		b.WriteString("[Peer]\n")
		b.WriteString("PublicKey = " + peer.PublicKey + "\n")
		if peer.Endpoint != "" {
			b.WriteString("Endpoint = " + peer.Endpoint + "\n")
			b.WriteString("PersistentKeepalive = 25\n")
		}
		b.WriteString("AllowedIPs = " + strings.Join(peer.AllowedIPs, ", ") + "\n")
		b.WriteString("\n")
	}
	return b.String()
}
