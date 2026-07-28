package control

import "time"

const (
	agentCommandStatePending = "pending"

	agentCommandTypeCollect           = "collect"
	agentCommandTypeApplyConfig       = "apply_config"
	agentCommandTypeApplyPeerConfig   = "apply_peer_config"
	agentCommandTypeUpdateAgent       = "update_agent"
	agentCommandTypeConnectivityCheck = "connectivity_check"
)

func newAgentCommand(tenantID, nodeID, commandType string) AgentCommand {
	return AgentCommand{
		ID:        newID("cmd"),
		TenantID:  tenantID,
		NodeID:    nodeID,
		Type:      commandType,
		State:     agentCommandStatePending,
		CreatedAt: time.Now(),
	}
}

func newAgentCommandsForNodes(tenantID, commandType string, nodes []Node) ([]AgentCommand, []string) {
	commands := make([]AgentCommand, 0, len(nodes))
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		commands = append(commands, newAgentCommand(tenantID, node.ID, commandType))
		nodeIDs = append(nodeIDs, node.ID)
	}
	return commands, nodeIDs
}
