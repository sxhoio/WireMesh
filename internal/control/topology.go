package control

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
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

// CompileOptions 携带拓扑编译所需的访问策略与路由扩展配置。
type CompileOptions struct {
	Resources []AccessResource
	Policies  []AccessPolicy
	Egress    *EgressConfig
}

func CompileTopology(network Network, nodes []Node, relations []PeerRelation, options CompileOptions, box *SecretBox) (map[string]NodeConfig, error) {
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
	extraAllowed := compileAccessAllowlist(network, nodes, options.Resources, options.Policies)
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
	if options.Egress != nil && options.Egress.EgressNodeID != "" {
		for _, node := range nodes {
			if node.ID == options.Egress.EgressNodeID {
				continue
			}
			key := node.ID + "\x00" + options.Egress.EgressNodeID
			extraAllowed[key] = append(extraAllowed[key], options.Egress.CIDRs...)
		}
	}
	// 中继：标记 wiremesh.relay=true 的节点作为中继；无法直连的节点对经中继互通。
	relayNodeIDs := map[string]bool{}
	for _, node := range nodes {
		if node.Labels["wiremesh.relay"] == "true" {
			relayNodeIDs[node.ID] = true
		}
	}
	if len(relayNodeIDs) > 0 {
		for _, a := range nodes {
			for _, b := range nodes {
				if a.ID >= b.ID || linked(a, b) {
					continue
				}
				for _, relay := range nodes {
					if !relayNodeIDs[relay.ID] || relay.ID == a.ID || relay.ID == b.ID || !linked(a, relay) || !linked(b, relay) {
						continue
					}
					extraAllowed[a.ID+"\x00"+relay.ID] = append(extraAllowed[a.ID+"\x00"+relay.ID], b.Address+"/32")
					extraAllowed[b.ID+"\x00"+relay.ID] = append(extraAllowed[b.ID+"\x00"+relay.ID], a.Address+"/32")
				}
			}
		}
	}
	for _, node := range nodes {
		config := configs[node.ID]
		for _, peer := range nodes {
			if node.ID == peer.ID || !linked(node, peer) {
				continue
			}
			allowed := []string{peer.Address + "/32"}
			allowed = append(allowed, extraAllowed[node.ID+"\x00"+peer.ID]...)
			config.Peers = append(config.Peers, PeerConfig{NodeID: peer.ID, PublicKey: peer.PublicKey, Endpoint: peer.Endpoint, AllowedIPs: dedupeStrings(allowed)})
		}
		configs[node.ID] = config
	}
	return configs, nil
}

// compileAccessAllowlist 计算访问策略允许的额外 CIDR，按「源节点 → 网关节点」分组。
// 这些 CIDR 会加入源节点配置中对应网关 Peer 的 AllowedIPs，实现 IP 级访问控制
// 与子网路由。
func compileAccessAllowlist(network Network, nodes []Node, resources []AccessResource, policies []AccessPolicy) map[string][]string {
	resourceByID := make(map[string]AccessResource, len(resources))
	for _, resource := range resources {
		if resource.NetworkID == network.ID {
			resourceByID[resource.ID] = resource
		}
	}
	out := make(map[string][]string)
	for _, policy := range policies {
		if !policy.Enabled || policy.NetworkID != network.ID {
			continue
		}
		for _, sourceID := range policySourceNodeIDs(policy, nodes) {
			for _, resourceID := range policy.ResourceIDs {
				resource, ok := resourceByID[resourceID]
				if !ok || resource.GatewayNodeID == sourceID {
					continue
				}
				key := sourceID + "\x00" + resource.GatewayNodeID
				out[key] = append(out[key], resource.Target)
			}
		}
	}
	for key := range out {
		out[key] = dedupeStrings(out[key])
	}
	return out
}

func policySourceNodeIDs(policy AccessPolicy, nodes []Node) []string {
	if label := strings.TrimSpace(policy.SourceLabel); label != "" {
		key, value, _ := strings.Cut(label, "=")
		out := make([]string, 0)
		for _, node := range nodes {
			if node.Labels[key] == value {
				out = append(out, node.ID)
			}
		}
		return out
	}
	// 未指定源节点且无标签时，策略作用于网络内全部节点。
	if len(policy.SourceNodeIDs) == 0 {
		out := make([]string, 0, len(nodes))
		for _, node := range nodes {
			out = append(out, node.ID)
		}
		return out
	}
	return policy.SourceNodeIDs
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
