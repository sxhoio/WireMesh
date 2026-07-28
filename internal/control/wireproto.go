package control

import (
	"time"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

func wireGuardStatusFromWire(interfaces []wireproto.WireGuardInterfaceStatus) []WireGuardInterfaceStatus {
	out := make([]WireGuardInterfaceStatus, 0, len(interfaces))
	for _, iface := range interfaces {
		peers := make([]WireGuardPeerStatus, 0, len(iface.Peers))
		for _, peer := range iface.Peers {
			peers = append(peers, WireGuardPeerStatus{
				PublicKey:           peer.PublicKey,
				Endpoint:            peer.Endpoint,
				AllowedIPs:          peer.AllowedIPs,
				LatestHandshakeAt:   parseWireTime(peer.LatestHandshakeAt),
				ReceiveBytes:        peer.ReceiveBytes,
				TransmitBytes:       peer.TransmitBytes,
				PersistentKeepalive: peer.PersistentKeepalive,
			})
		}
		out = append(out, WireGuardInterfaceStatus{
			Name:       iface.Name,
			PublicKey:  iface.PublicKey,
			ListenPort: iface.ListenPort,
			Addresses:  iface.Addresses,
			MTU:        iface.MTU,
			Up:         iface.Up,
			Peers:      peers,
		})
	}
	if out == nil {
		return []WireGuardInterfaceStatus{}
	}
	return out
}

func peerConfigFilesFromWire(files []wireproto.PeerConfigFile) []PeerConfigFile {
	out := make([]PeerConfigFile, 0, len(files))
	for _, file := range files {
		out = append(out, PeerConfigFile{
			Interface: file.Interface,
			Path:      file.Path,
			Content:   file.Content,
			UpdatedAt: parseWireTime(file.UpdatedAt),
		})
	}
	if out == nil {
		return []PeerConfigFile{}
	}
	return out
}

func peerConfigFilesToWire(files []PeerConfigFile) []wireproto.PeerConfigFile {
	out := make([]wireproto.PeerConfigFile, 0, len(files))
	for _, file := range files {
		out = append(out, wireproto.PeerConfigFile{
			Interface: file.Interface,
			Path:      file.Path,
			Content:   file.Content,
			UpdatedAt: formatWireTime(file.UpdatedAt),
		})
	}
	if out == nil {
		return []wireproto.PeerConfigFile{}
	}
	return out
}

func agentCommandsToWire(commands []AgentCommand) []wireproto.AgentCommand {
	out := make([]wireproto.AgentCommand, 0, len(commands))
	for _, command := range commands {
		out = append(out, wireproto.AgentCommand{
			ID:    command.ID,
			Type:  command.Type,
			State: command.State,
		})
	}
	if out == nil {
		return []wireproto.AgentCommand{}
	}
	return out
}

func parseWireTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func formatWireTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
