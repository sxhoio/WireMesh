package control

import (
	"strings"
	"testing"
	"time"
)

func TestNewAgentCommandDefaults(t *testing.T) {
	before := time.Now()
	command := newAgentCommand("tenant-1", "node-1", agentCommandTypeCollect)
	after := time.Now()

	if !strings.HasPrefix(command.ID, "cmd_") {
		t.Fatalf("command ID = %q, want cmd_ prefix", command.ID)
	}
	if command.TenantID != "tenant-1" || command.NodeID != "node-1" || command.Type != agentCommandTypeCollect {
		t.Fatalf("unexpected command identity: %#v", command)
	}
	if command.State != agentCommandStatePending {
		t.Fatalf("command state = %q, want %q", command.State, agentCommandStatePending)
	}
	if command.CreatedAt.Before(before) || command.CreatedAt.After(after) {
		t.Fatalf("command CreatedAt = %s, want between %s and %s", command.CreatedAt, before, after)
	}
}

func TestNewAgentCommandsForNodesPreservesNodeOrder(t *testing.T) {
	nodes := []Node{{ID: "node-a"}, {ID: "node-b"}}

	commands, nodeIDs := newAgentCommandsForNodes("tenant-1", agentCommandTypeUpdateAgent, nodes)

	if len(commands) != 2 || len(nodeIDs) != 2 {
		t.Fatalf("commands=%d nodeIDs=%d, want 2 each", len(commands), len(nodeIDs))
	}
	for index, id := range []string{"node-a", "node-b"} {
		if nodeIDs[index] != id || commands[index].NodeID != id {
			t.Fatalf("index %d command=%#v nodeID=%q, want %q", index, commands[index], nodeIDs[index], id)
		}
		if commands[index].Type != agentCommandTypeUpdateAgent || commands[index].State != agentCommandStatePending {
			t.Fatalf("unexpected command at index %d: %#v", index, commands[index])
		}
	}
}
