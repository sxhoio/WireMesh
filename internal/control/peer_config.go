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
		writeError(w, http.StatusNotFound, "node not found")
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
		writeError(w, http.StatusNotFound, "node not found")
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
		writeError(w, http.StatusBadRequest, "interface must be a valid WireGuard interface name")
		return
	}
	content := wgconfig.NormalizePeerConfig(in.Content)
	if len(content) > maxPeerConfigContentBytes {
		writeError(w, http.StatusBadRequest, "peer config is too large")
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
		writeError(w, http.StatusInternalServerError, "failed to save peer config")
		return
	}
	command := newAgentCommand(c.TenantID, node.ID, agentCommandTypeApplyPeerConfig)
	if err := a.createAgentCommand(command); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue peer config delivery")
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

func (a *App) recordPeerConfigCommandResult(node *Node, state string) error {
	if state != "completed" || len(node.DesiredPeerConfig) == 0 {
		return nil
	}
	applied := make([]PeerConfigFile, len(node.DesiredPeerConfig))
	copy(applied, node.DesiredPeerConfig)
	now := time.Now()
	for i := range applied {
		if applied[i].UpdatedAt.IsZero() {
			applied[i].UpdatedAt = now
		}
	}
	node.PeerConfigFiles = applied
	node.DesiredPeerConfig = []PeerConfigFile{}
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
