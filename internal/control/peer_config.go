package control

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
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
	if !validPeerConfigInterfaceName(iface) {
		writeError(w, http.StatusBadRequest, "interface must be a valid WireGuard interface name")
		return
	}
	content := normalizePeerConfigContent(in.Content)
	if len(content) > maxPeerConfigContentBytes {
		writeError(w, http.StatusBadRequest, "peer config is too large")
		return
	}
	if err := validatePeerConfigContent(content); err != nil {
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
	command := AgentCommand{ID: newID("cmd"), TenantID: c.TenantID, NodeID: node.ID, Type: "apply_peer_config", State: "pending", CreatedAt: time.Now()}
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
	writeJSON(w, http.StatusOK, NodePeerConfigResponse{NodeID: node.ID, Files: peerConfigFilesOrEmpty(node.DesiredPeerConfig), HasPending: true})
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
		if !validPeerConfigInterfaceName(iface) || seen[iface] {
			continue
		}
		content := normalizePeerConfigContent(file.Content)
		if len(content) > maxPeerConfigContentBytes || peerConfigContainsSecretOrInterface(content) {
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

func normalizePeerConfigContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimSpace(content)
}

func peerConfigContainsSecretOrInterface(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "[interface]") || strings.Contains(lower, "privatekey")
}

func validatePeerConfigContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if peerConfigContainsSecretOrInterface(content) {
		return fmt.Errorf("peer config must only contain [Peer] sections and must not contain Interface or PrivateKey")
	}
	inPeer := false
	peerIndex := 0
	hasPublicKey := false
	hasAllowedIPs := false
	checkPeer := func() error {
		if peerIndex == 0 {
			return nil
		}
		if !hasPublicKey {
			return fmt.Errorf("peer %d is missing PublicKey", peerIndex)
		}
		if !hasAllowedIPs {
			return fmt.Errorf("peer %d is missing AllowedIPs", peerIndex)
		}
		return nil
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if !strings.EqualFold(trimmed, "[Peer]") {
				return fmt.Errorf("peer config must only contain [Peer] sections")
			}
			if err := checkPeer(); err != nil {
				return err
			}
			inPeer = true
			peerIndex++
			hasPublicKey = false
			hasAllowedIPs = false
			continue
		}
		if !inPeer {
			return fmt.Errorf("peer config must start with a [Peer] section")
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return fmt.Errorf("invalid peer config line %q", trimmed)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "publickey":
			if err := validatePeerWireGuardKey(value); err != nil {
				return fmt.Errorf("peer %d has an invalid PublicKey", peerIndex)
			}
			hasPublicKey = true
		case "presharedkey":
			if err := validatePeerWireGuardKey(value); err != nil {
				return fmt.Errorf("peer %d has an invalid PresharedKey", peerIndex)
			}
		case "allowedips":
			if value == "" {
				return fmt.Errorf("peer %d has empty AllowedIPs", peerIndex)
			}
			for _, allowed := range strings.Split(value, ",") {
				if _, err := netip.ParsePrefix(strings.TrimSpace(allowed)); err != nil {
					return fmt.Errorf("peer %d has an invalid AllowedIPs entry", peerIndex)
				}
			}
			hasAllowedIPs = true
		case "endpoint":
			if strings.ContainsAny(value, "\r\n") {
				return fmt.Errorf("peer %d has an invalid Endpoint", peerIndex)
			}
		case "persistentkeepalive":
			keepalive, err := strconv.Atoi(value)
			if err != nil || keepalive < 0 || keepalive > 65535 {
				return fmt.Errorf("peer %d has an invalid PersistentKeepalive", peerIndex)
			}
		default:
			return fmt.Errorf("unsupported peer config key %q", key)
		}
	}
	return checkPeer()
}

func validatePeerWireGuardKey(value string) error {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("invalid WireGuard key")
	}
	return nil
}

func validPeerConfigInterfaceName(value string) bool {
	if value == "" || len(value) > 15 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("_.-", character) {
			continue
		}
		return false
	}
	return true
}
