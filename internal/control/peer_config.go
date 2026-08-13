package control

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/wgconfig"
	"github.com/wiremesh/wiremesh/internal/wireproto"
)

const maxPeerConfigContentBytes = 128 * 1024

type NodePeerConfigResponse struct {
	NodeID       string           `json:"node_id"`
	Files        []PeerConfigFile `json:"files"`
	PendingFiles []PeerConfigFile `json:"pending_files,omitempty"`
	HasPending   bool             `json:"has_pending"`
}

type NodePeerConfigUpdateResult struct {
	NodeID  string           `json:"node_id"`
	Files   []PeerConfigFile `json:"files"`
	Command AgentCommand     `json:"command"`
	Offline bool             `json:"offline"`
	Message string           `json:"message"`
}

func (a *App) nodePeerConfig(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}
	writeJSON(w, http.StatusOK, NodePeerConfigResponse{
		NodeID:       node.ID,
		Files:        peerConfigFilesOrEmpty(node.PeerConfigFiles),
		PendingFiles: peerConfigFilesOrEmpty(node.DesiredPeerConfig),
		HasPending:   len(node.DesiredPeerConfig) > 0,
	})
}

func (a *App) updateNodePeerConfig(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}
	var in struct {
		Interface string `json:"interface"`
		Content   string `json:"content"`
	}
	if !decode(w, r, &in) {
		return
	}
	iface := strings.TrimSpace(in.Interface)
	if iface == "" && len(node.PeerConfigFiles) == 1 {
		iface = node.PeerConfigFiles[0].Interface
	}
	if !wgconfig.ValidInterfaceName(iface) {
		writeError(w, http.StatusBadRequest, "接口必须是有效的 WireGuard 接口名称")
		return
	}
	content := wgconfig.NormalizePeerConfig(in.Content)
	if len(content) > maxPeerConfigContentBytes {
		writeError(w, http.StatusBadRequest, "Peer 配置内容过大")
		return
	}
	if err := wgconfig.ValidatePeerConfig(content); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file := PeerConfigFile{
		Interface: iface,
		Path:      peerConfigPathFor(node, iface),
		Content:   content,
		UpdatedAt: time.Now(),
	}
	node.DesiredPeerConfig = upsertPeerConfigFile(node.DesiredPeerConfig, file)
	node = normalizeNodeDefaults(node)
	if err := a.store.UpdateNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Peer 配置失败")
		return
	}
	command := newAgentCommand(c.TenantID, node.ID, agentCommandTypeApplyPeerConfig)
	if err := a.createAgentCommand(command); err != nil {
		writeError(w, http.StatusInternalServerError, "Peer 配置下发失败")
		return
	}
	offline := a.nodeCurrentlyOffline(c.TenantID, node)
	message := "Peer 配置已保存并下发"
	if offline {
		message = "Peer 配置已保存；客户端不在线，配置将在上线后下发"
	}
	a.auditEvent(c.TenantID, c.Subject, "agent.peer_config.save", "node", node.ID, map[string]string{"interface": iface, "offline": fmt.Sprint(offline), "command_id": command.ID})
	writeJSON(w, http.StatusAccepted, NodePeerConfigUpdateResult{NodeID: node.ID, Files: peerConfigFilesOrEmpty(node.DesiredPeerConfig), Command: command, Offline: offline, Message: message})
}

func (a *App) agentPeerConfig(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	if !node.Enabled {
		writeError(w, http.StatusLocked, "node is disabled")
		return
	}
	if len(node.DesiredPeerConfig) == 0 {
		writeError(w, http.StatusNotFound, "no pending peer config")
		return
	}
	a.auditEvent(node.TenantID, node.ID, "agent.peer_config.read", "node", node.ID, map[string]string{"files": fmt.Sprint(len(node.DesiredPeerConfig))})
	writeJSON(w, http.StatusOK, wireproto.PeerConfigResponse{NodeID: node.ID, Files: peerConfigFilesToWire(peerConfigFilesOrEmpty(node.DesiredPeerConfig))})
}

// recordPeerConfigCommandResult 把命令下发时刻（commandIssuedAt）之前的期望配置
// 标记为已应用。命令执行期间新下发的配置（UpdatedAt 晚于命令创建时间）保持
// 待下发状态，避免被旧命令的完成结果误标为已应用。
func (a *App) recordPeerConfigCommandResult(node *Node, state string, commandIssuedAt time.Time) error {
	if state != "completed" || len(node.DesiredPeerConfig) == 0 {
		return nil
	}
	now := time.Now()
	applied := make([]PeerConfigFile, 0, len(node.DesiredPeerConfig))
	remaining := make([]PeerConfigFile, 0, len(node.DesiredPeerConfig))
	for _, file := range node.DesiredPeerConfig {
		if !file.UpdatedAt.IsZero() && file.UpdatedAt.After(commandIssuedAt) {
			remaining = append(remaining, file)
			continue
		}
		entry := file
		if entry.UpdatedAt.IsZero() {
			entry.UpdatedAt = now
		}
		applied = append(applied, entry)
	}
	node.PeerConfigFiles = append(node.PeerConfigFiles, applied...)
	node.DesiredPeerConfig = remaining
	node.LastSeen = now
	return a.store.UpdateNode(normalizeNodeDefaults(*node))
}

func sanitizeAgentPeerConfigFiles(files []PeerConfigFile, updatedAt time.Time) []PeerConfigFile {
	out := make([]PeerConfigFile, 0, len(files))
	seen := map[string]bool{}
	for _, file := range files {
		iface := strings.TrimSpace(file.Interface)
		if !wgconfig.ValidInterfaceName(iface) || seen[iface] {
			continue
		}
		content := wgconfig.NormalizePeerConfig(file.Content)
		if len(content) > maxPeerConfigContentBytes || wgconfig.PeerConfigContainsSecretOrInterface(content) {
			continue
		}
		when := file.UpdatedAt
		if when.IsZero() {
			when = updatedAt
		}
		out = append(out, PeerConfigFile{
			Interface: iface,
			Path:      strings.TrimSpace(file.Path),
			Content:   content,
			UpdatedAt: when,
		})
		seen[iface] = true
	}
	if out == nil {
		return []PeerConfigFile{}
	}
	return out
}

func (a *App) nodeCurrentlyOffline(tenantID string, node Node) bool {
	settings, err := a.tenantSettings(tenantID)
	offlineAfter := 90 * time.Second
	if err == nil && settings.StatusRules.AgentOfflineSec > 0 {
		offlineAfter = time.Duration(settings.StatusRules.AgentOfflineSec) * time.Second
	}
	return node.LastSeen.IsZero() || time.Since(node.LastSeen) > offlineAfter
}

func peerConfigPathFor(node Node, iface string) string {
	for _, file := range node.DesiredPeerConfig {
		if file.Interface == iface && strings.TrimSpace(file.Path) != "" {
			return strings.TrimSpace(file.Path)
		}
	}
	for _, file := range node.PeerConfigFiles {
		if file.Interface == iface && strings.TrimSpace(file.Path) != "" {
			return strings.TrimSpace(file.Path)
		}
	}
	return "/etc/wireguard/" + iface + ".conf"
}

func peerConfigFilesOrEmpty(files []PeerConfigFile) []PeerConfigFile {
	if files == nil {
		return []PeerConfigFile{}
	}
	return files
}

func upsertPeerConfigFile(files []PeerConfigFile, file PeerConfigFile) []PeerConfigFile {
	out := make([]PeerConfigFile, 0, len(files)+1)
	replaced := false
	for _, current := range files {
		if current.Interface == file.Interface {
			out = append(out, file)
			replaced = true
			continue
		}
		out = append(out, current)
	}
	if !replaced {
		out = append(out, file)
	}
	return out
}
