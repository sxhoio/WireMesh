package control

import (
	"fmt"
	"net/netip"
	"sort"
)

func AllocateAddress(cidr string, allocated []string) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid network CIDR: %w", err)
	}
	if !prefix.Addr().Is4() {
		return "", fmt.Errorf("only IPv4 address pools are currently supported")
	}
	used := map[netip.Addr]bool{}
	for _, raw := range allocated {
		if addr, err := netip.ParseAddr(raw); err == nil {
			used[addr] = true
		}
	}
	prefix = prefix.Masked()
	broadcast := lastIPv4Address(prefix)
	for candidate := prefix.Addr().Next(); prefix.Contains(candidate); candidate = candidate.Next() {
		if prefix.Bits() < 31 && candidate == broadcast {
			break
		}
		if !used[candidate] {
			return candidate.String(), nil
		}
	}
	return "", fmt.Errorf("address pool %s is exhausted", cidr)
}

func CompileTopology(network Network, nodes []Node, relations []PeerRelation, box *SecretBox) (map[string]NodeConfig, error) {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	configs := make(map[string]NodeConfig, len(nodes))
	for _, node := range nodes {
		privateKey, err := box.Decrypt(node.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt node %s key: %w", node.ID, err)
		}
		node = normalizeNodeDefaults(node)
		configs[node.ID] = NodeConfig{NodeID: node.ID, NetworkID: network.ID, Address: node.Address, PrivateKey: string(privateKey), ListenPort: node.ListenPort, MTU: node.MTU}
	}
	relationSet := topologyRelationSet(network, relations)
	linked := func(a, b Node) bool {
		switch network.Topology {
		case TopologyFullMesh:
			return true
		case TopologyHubSpoke:
			return a.Labels["wiremesh.role"] == "hub" || b.Labels["wiremesh.role"] == "hub"
		case TopologyCustom:
			return relationSet[[2]string{a.ID, b.ID}]
		}
		return false
	}
	for _, node := range nodes {
		config := configs[node.ID]
		for _, peer := range nodes {
			if node.ID == peer.ID || !linked(node, peer) {
				continue
			}
			config.Peers = append(config.Peers, PeerConfig{NodeID: peer.ID, PublicKey: peer.PublicKey, Endpoint: peer.Endpoint, AllowedIPs: []string{peer.Address + "/32"}})
		}
		configs[node.ID] = config
	}
	return configs, nil
}

func topologyRelationSet(network Network, relations []PeerRelation) map[[2]string]bool {
	if network.Topology != TopologyCustom {
		return nil
	}
	out := make(map[[2]string]bool, len(relations)*2)
	for _, relation := range relations {
		out[[2]string{relation.SourceNodeID, relation.TargetNodeID}] = true
		out[[2]string{relation.TargetNodeID, relation.SourceNodeID}] = true
	}
	return out
}
