package control

import (
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const (
	defaultNodeListenPort = 51820
	defaultNodeMTU        = 1420
	maxCommandWait        = 25 * time.Second
)

func normalizeNodeDefaults(node Node) Node {
	if node.ListenPort == 0 {
		node.ListenPort = defaultNodeListenPort
	}
	if node.MTU == 0 {
		node.MTU = defaultNodeMTU
	}
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	if node.WireGuard == nil {
		node.WireGuard = []WireGuardInterfaceStatus{}
	}
	if node.PeerConfigFiles == nil {
		node.PeerConfigFiles = []PeerConfigFile{}
	}
	if node.DesiredPeerConfig == nil {
		node.DesiredPeerConfig = []PeerConfigFile{}
	}
	return node
}

func (a *App) adoptReportedNodeConfiguration(node *Node) (string, bool) {
	iface, address, ok := a.reportedNodeConfiguration(*node)
	if !ok {
		return "", false
	}
	listenPort := node.ListenPort
	if iface.ListenPort >= 1 && iface.ListenPort <= 65535 {
		listenPort = iface.ListenPort
	}
	mtu := node.MTU
	if iface.MTU >= 576 && iface.MTU <= 9000 {
		mtu = iface.MTU
	}
	if node.Address == address && node.ListenPort == listenPort && node.MTU == mtu {
		return "", false
	}
	if len(a.store.ListDeliveries(node.TenantID, node.ID)) != 0 {
		return "", false
	}
	for _, event := range a.store.ListAudit(node.TenantID) {
		if event.ResourceType == "node" && event.ResourceID == node.ID && (event.Action == "node.update" || event.Action == "agent.config.observed") {
			return "", false
		}
	}
	for _, other := range a.store.ListNodes(node.TenantID, node.NetworkID) {
		if other.ID != node.ID && other.Address == address {
			return "", false
		}
	}
	node.Address = address
	node.ListenPort = listenPort
	node.MTU = mtu
	return iface.Name, true
}

func (a *App) reportedNodeConfiguration(node Node) (WireGuardInterfaceStatus, string, bool) {
	for _, iface := range node.WireGuard {
		for _, raw := range iface.Addresses {
			address := strings.TrimSpace(raw)
			if value, _, found := strings.Cut(address, "/"); found {
				address = value
			}
			if a.validateNodeAddress(node, address) == nil {
				return iface, address, true
			}
		}
	}
	return WireGuardInterfaceStatus{}, "", false
}

func (a *App) nodeByID(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	writeJSON(w, http.StatusOK, normalizeNodeDefaults(node))
}

func (a *App) updateNode(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	var in struct {
		Name              *string            `json:"name"`
		Address           *string            `json:"address"`
		Endpoint          *string            `json:"endpoint"`
		ListenPort        *int               `json:"listen_port"`
		MTU               *int               `json:"mtu"`
		Enabled           *bool              `json:"enabled"`
		InterfaceSelector *string            `json:"interface_selector"`
		Labels            *map[string]string `json:"labels"`
		LocationName      *string            `json:"location_name"`
		LocationSource    *string            `json:"location_source"`
		Latitude          *float64           `json:"latitude"`
		Longitude         *float64           `json:"longitude"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name != nil {
		value := strings.TrimSpace(*in.Name)
		if value == "" {
			writeError(w, http.StatusBadRequest, "node name is required")
			return
		}
		node.Name = value
	}
	if in.Address != nil {
		value := strings.TrimSpace(*in.Address)
		if err := a.validateNodeAddress(node, value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		for _, other := range a.store.ListNodes(c.TenantID, node.NetworkID) {
			if other.ID != node.ID && other.Address == value {
				writeError(w, http.StatusConflict, "address is already used by another node")
				return
			}
		}
		node.Address = value
	}
	if in.Endpoint != nil {
		node.Endpoint = strings.TrimSpace(*in.Endpoint)
	}
	if in.ListenPort != nil {
		if *in.ListenPort < 1 || *in.ListenPort > 65535 {
			writeError(w, http.StatusBadRequest, "listen_port must be between 1 and 65535")
			return
		}
		node.ListenPort = *in.ListenPort
	}
	if in.MTU != nil {
		if *in.MTU < 576 || *in.MTU > 9000 {
			writeError(w, http.StatusBadRequest, "mtu must be between 576 and 9000")
			return
		}
		node.MTU = *in.MTU
	}
	if in.Enabled != nil {
		node.Enabled = *in.Enabled
	}
	if in.InterfaceSelector != nil {
		node.InterfaceSelector = strings.TrimSpace(*in.InterfaceSelector)
	}
	if in.Labels != nil {
		node.Labels = *in.Labels
		if node.Labels == nil {
			node.Labels = map[string]string{}
		}
	}
	if in.LocationSource != nil || in.LocationName != nil || in.Latitude != nil || in.Longitude != nil {
		source := node.LocationSource
		if in.LocationSource != nil {
			source = strings.TrimSpace(*in.LocationSource)
		}
		switch source {
		case "":
			node.LocationName = ""
			node.LocationSource = ""
			node.Latitude = 0
			node.Longitude = 0
		case "manual":
			locationName := node.LocationName
			if in.LocationName != nil {
				locationName = strings.TrimSpace(*in.LocationName)
			}
			if locationName == "" {
				writeError(w, http.StatusBadRequest, "location_name is required for manual location")
				return
			}

			latitude, longitude := resolveManualLocation(locationName, node.Latitude, node.Longitude)
			if in.Latitude != nil {
				latitude = *in.Latitude
			}
			if in.Longitude != nil {
				longitude = *in.Longitude
			}
			if math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 {
				writeError(w, http.StatusBadRequest, "latitude must be between -90 and 90")
				return
			}
			if math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
				writeError(w, http.StatusBadRequest, "longitude must be between -180 and 180")
				return
			}
			node.LocationName = locationName
			node.LocationSource = "manual"
			node.Latitude = latitude
			node.Longitude = longitude
		default:
			writeError(w, http.StatusBadRequest, "location_source must be manual or empty")
			return
		}
	}
	node = normalizeNodeDefaults(node)
	if err := a.store.UpdateNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update node")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "node.update", "node", node.ID, map[string]string{"address": node.Address, "enabled": fmt.Sprint(node.Enabled), "location_source": node.LocationSource})
	var delivery *ConfigPublishResult
	deliveryError := ""
	if network, err := a.store.GetNetwork(c.TenantID, node.NetworkID); err == nil {
		if result, err := a.publishNetwork(c.TenantID, network); err != nil {
			deliveryError = err.Error()
		} else {
			delivery = &result
			action := "config.publish.auto"
			if result.Unchanged {
				action = "config.publish.auto.noop"
			}
			a.auditEvent(c.TenantID, c.Subject, action, "network", network.ID, map[string]string{
				"node_id": node.ID, "version": fmt.Sprint(result.Version),
				"changed_nodes": fmt.Sprint(len(result.ChangedNodeIDs)), "offline_nodes": fmt.Sprint(len(result.OfflineNodeIDs)),
			})
		}
	} else {
		deliveryError = "node network not found"
	}
	writeJSON(w, http.StatusOK, struct {
		Node
		Delivery      *ConfigPublishResult `json:"delivery,omitempty"`
		DeliveryError string               `json:"delivery_error,omitempty"`
	}{Node: node, Delivery: delivery, DeliveryError: deliveryError})
}

func (a *App) validateNodeAddress(node Node, value string) error {
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() {
		return fmt.Errorf("address must be a valid IPv4 address")
	}
	network, err := a.store.GetNetwork(node.TenantID, node.NetworkID)
	if err != nil {
		return fmt.Errorf("node network not found")
	}
	prefix, err := netip.ParsePrefix(network.CIDR)
	if err != nil || !prefix.Addr().Is4() {
		return fmt.Errorf("network CIDR is invalid")
	}
	prefix = prefix.Masked()
	if !prefix.Contains(address) {
		return fmt.Errorf("address must be inside network %s", network.CIDR)
	}
	if address == prefix.Addr() {
		return fmt.Errorf("network address cannot be assigned to a node")
	}
	if prefix.Bits() < 31 && address == lastIPv4Address(prefix) {
		return fmt.Errorf("broadcast address cannot be assigned to a node")
	}
	return nil
}

func lastIPv4Address(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Masked().Addr().As4()
	hostBits := 32 - prefix.Bits()
	value := uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
	value |= uint32(1<<hostBits) - 1
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}

func (a *App) deleteNode(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if err := a.store.DeleteNode(c.TenantID, node.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete node")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "node.delete", "node", node.ID, map[string]string{"name": node.Name})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) createNodeCommand(commandType string) func(http.ResponseWriter, *http.Request, claims) {
	return func(w http.ResponseWriter, r *http.Request, c claims) {
		node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		command := AgentCommand{ID: newID("cmd"), TenantID: c.TenantID, NodeID: node.ID, Type: commandType, State: "pending", CreatedAt: time.Now()}
		if err := a.createAgentCommand(command); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create agent command")
			return
		}
		a.auditEvent(c.TenantID, c.Subject, "agent.command."+commandType, "node", node.ID, map[string]string{"command_id": command.ID})
		writeJSON(w, http.StatusAccepted, command)
	}
}

// collectNodes fans a collect command out to many nodes at once. Connected
// Agents are woken through their command long-poll and report fresh data
// without waiting for the periodic configuration probe.
func (a *App) collectNodes(w http.ResponseWriter, r *http.Request, c claims) {
	var in struct {
		NodeIDs []string `json:"node_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	targets := make([]Node, 0, len(in.NodeIDs))
	if len(in.NodeIDs) == 0 {
		for _, node := range a.store.ListNodes(c.TenantID, "") {
			if node.Enabled {
				targets = append(targets, node)
			}
		}
	} else {
		for _, id := range in.NodeIDs {
			node, err := a.store.GetNode(c.TenantID, id)
			if err != nil {
				continue
			}
			targets = append(targets, node)
		}
	}
	if len(targets) == 0 {
		writeJSON(w, http.StatusAccepted, map[string]any{"created": 0, "node_ids": []string{}})
		return
	}
	created := 0
	nodeIDs := make([]string, 0, len(targets))
	for _, node := range targets {
		command := AgentCommand{ID: newID("cmd"), TenantID: c.TenantID, NodeID: node.ID, Type: "collect", State: "pending", CreatedAt: time.Now()}
		if err := a.createAgentCommand(command); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create agent command")
			return
		}
		created++
		nodeIDs = append(nodeIDs, node.ID)
	}
	a.auditEvent(c.TenantID, c.Subject, "agent.command.collect_all", "tenant", c.TenantID, map[string]string{"count": fmt.Sprint(created)})
	writeJSON(w, http.StatusAccepted, map[string]any{"created": created, "node_ids": nodeIDs})
}

func (a *App) nodeLogs(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	limit, offset := parseLogPage(r)
	errorsOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("level")), "error")
	commands := a.store.ListCommandsPage(c.TenantID, node.ID, limit+1, offset, errorsOnly)
	logs := make([]NodeLog, 0)
	for _, command := range commands {
		message := command.Type + " · " + command.State
		if command.Result != "" {
			message += " · " + command.Result
		}
		created := command.CreatedAt
		if command.CompletedAt != nil {
			created = *command.CompletedAt
		}
		level := "info"
		if command.State == "failed" {
			level = "error"
		}
		logs = append(logs, NodeLog{ID: command.ID, Level: level, Source: "command", Message: message, CreatedAt: created})
	}
	hasMore := len(commands) > limit
	if hasMore {
		logs = logs[:limit]
	}
	if logs == nil {
		logs = []NodeLog{}
	}
	writeJSON(w, http.StatusOK, NodeLogPage{Items: logs, CurrentError: node.CollectionError, Limit: limit, Offset: offset, HasMore: hasMore})
}

func (a *App) clearNodeLogs(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if err := a.store.ClearCommands(c.TenantID, node.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear Agent logs")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) agentCommands(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	waitFor, err := parseCommandWait(r.URL.Query().Get("wait"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	commands := a.store.ClaimCommands(node.ID)
	if len(commands) == 0 && waitFor > 0 {
		timer := time.NewTimer(waitFor)
		select {
		case <-r.Context().Done():
			timer.Stop()
			return
		case <-a.commandWakeup(node.ID):
			timer.Stop()
		case <-timer.C:
		}
		commands = a.store.ClaimCommands(node.ID)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-WireMesh-Command-Long-Poll", "true")
	writeJSON(w, http.StatusOK, commands)
}

func (a *App) createAgentCommand(command AgentCommand) error {
	if err := a.store.CreateCommand(command); err != nil {
		return err
	}
	a.wakeAgentCommand(command.NodeID)
	return nil
}

func (a *App) commandWakeup(nodeID string) chan struct{} {
	a.commandMu.Lock()
	defer a.commandMu.Unlock()
	if wakeup := a.commandWakeups[nodeID]; wakeup != nil {
		return wakeup
	}
	wakeup := make(chan struct{}, 1)
	a.commandWakeups[nodeID] = wakeup
	return wakeup
}

func (a *App) wakeAgentCommand(nodeID string) {
	select {
	case a.commandWakeup(nodeID) <- struct{}{}:
	default:
	}
}

func parseCommandWait(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	waitFor, err := time.ParseDuration(value)
	if err != nil || waitFor < 0 {
		return 0, fmt.Errorf("wait must be a non-negative duration")
	}
	if waitFor > maxCommandWait {
		waitFor = maxCommandWait
	}
	return waitFor, nil
}

func (a *App) agentCommandResult(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	var in struct {
		State  string `json:"state"`
		Result string `json:"result"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.State != "completed" && in.State != "failed" {
		writeError(w, http.StatusBadRequest, "invalid command state")
		return
	}
	var command *AgentCommand
	for _, current := range a.store.ListCommands(node.TenantID, node.ID) {
		if current.ID == r.PathValue("id") {
			value := current
			command = &value
			break
		}
	}
	if command == nil {
		writeError(w, http.StatusNotFound, "command not found")
		return
	}
	now := time.Now()
	command.State = in.State
	command.Result = strings.TrimSpace(in.Result)
	command.CompletedAt = &now
	if err := a.store.UpdateCommand(*command); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update command")
		return
	}
	if command.Type == "apply_peer_config" {
		if err := a.recordPeerConfigCommandResult(&node, in.State); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record peer config result")
			return
		}
	}
	a.auditEvent(node.TenantID, node.ID, "agent.command."+in.State, "node", node.ID, map[string]string{"command_id": command.ID, "type": command.Type})
	writeJSON(w, http.StatusOK, command)
}
