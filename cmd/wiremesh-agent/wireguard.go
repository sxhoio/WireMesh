package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/wgconfig"
	"github.com/wiremesh/wiremesh/internal/wireproto"
)

const defaultAgentMTU = 1420

type wireGuardPeerStatus = wireproto.WireGuardPeerStatus
type wireGuardInterfaceStatus = wireproto.WireGuardInterfaceStatus
type peerConfig = wireproto.PeerConfig
type nodeConfig = wireproto.NodeConfig
type configResponse = wireproto.ConfigResponse

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("required command %s is not installed", name)
		}
		return nil, fmt.Errorf("%s command failed: %w", name, err)
	}
	return output, nil
}

func collectWireGuard(parent context.Context, selector string, runner commandRunner) ([]wireGuardInterfaceStatus, string) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()

	dump, err := runner.Run(ctx, "wg", "show", "all", "dump")
	if err != nil {
		return []wireGuardInterfaceStatus{}, err.Error()
	}
	interfaces, err := parseWireGuardDump(string(dump), selector)
	if err != nil {
		return []wireGuardInterfaceStatus{}, err.Error()
	}
	warnings := make([]string, 0, 2)
	if output, commandErr := runner.Run(ctx, "ip", "-j", "address", "show"); commandErr != nil {
		warnings = append(warnings, commandErr.Error())
	} else if parseErr := mergeAddressMetadata(interfaces, output); parseErr != nil {
		warnings = append(warnings, "parse interface addresses: "+parseErr.Error())
	}
	if output, commandErr := runner.Run(ctx, "ip", "-j", "link", "show"); commandErr != nil {
		warnings = append(warnings, commandErr.Error())
	} else if parseErr := mergeLinkMetadata(interfaces, output); parseErr != nil {
		warnings = append(warnings, "parse interface links: "+parseErr.Error())
	}
	return interfaces, strings.Join(warnings, "; ")
}

func parseWireGuardDump(value, selector string) ([]wireGuardInterfaceStatus, error) {
	selected, all, err := parseInterfaceSelector(selector)
	if err != nil {
		return nil, err
	}
	byName := map[string]*wireGuardInterfaceStatus{}
	scanner := bufio.NewScanner(strings.NewReader(value))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "	")
		if len(fields) != 5 && len(fields) < 9 {
			return nil, fmt.Errorf("unexpected wg dump row with %d fields", len(fields))
		}
		name := fields[0]
		if !all && !selected[name] {
			continue
		}
		if len(fields) == 5 {
			listenPort, parseErr := strconv.Atoi(fields[3])
			if parseErr != nil {
				return nil, fmt.Errorf("invalid listen port for %s", name)
			}
			byName[name] = &wireGuardInterfaceStatus{
				Name: name, PublicKey: dashToEmpty(fields[2]), ListenPort: listenPort,
				Addresses: []string{}, Peers: []wireGuardPeerStatus{},
			}
			continue
		}
		iface := byName[name]
		if iface == nil {
			iface = &wireGuardInterfaceStatus{Name: name, Addresses: []string{}, Peers: []wireGuardPeerStatus{}}
			byName[name] = iface
		}
		handshake, parseErr := strconv.ParseInt(fields[5], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid handshake timestamp for %s", name)
		}
		receiveBytes, parseErr := strconv.ParseInt(fields[6], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid receive counter for %s", name)
		}
		transmitBytes, parseErr := strconv.ParseInt(fields[7], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid transmit counter for %s", name)
		}
		keepalive, parseErr := parsePersistentKeepalive(fields[8])
		if parseErr != nil {
			return nil, fmt.Errorf("invalid keepalive for %s: %w", name, parseErr)
		}
		peer := wireGuardPeerStatus{
			PublicKey: dashToEmpty(fields[1]), Endpoint: dashToEmpty(fields[3]),
			AllowedIPs: splitComma(fields[4]), ReceiveBytes: receiveBytes,
			TransmitBytes: transmitBytes, PersistentKeepalive: keepalive,
		}
		if handshake > 0 {
			peer.LatestHandshakeAt = time.Unix(handshake, 0).UTC().Format(time.RFC3339)
		}
		iface.Peers = append(iface.Peers, peer)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]wireGuardInterfaceStatus, 0, len(names))
	for _, name := range names {
		result = append(result, *byName[name])
	}
	return result, nil
}

func parsePersistentKeepalive(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "off") {
		return 0, nil
	}
	keepalive, err := strconv.Atoi(value)
	if err != nil || keepalive < 0 {
		return 0, fmt.Errorf("unexpected value %q", value)
	}
	return keepalive, nil
}

func parseInterfaceSelector(value string) (map[string]bool, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "auto") || value == "*" {
		return nil, true, nil
	}
	selected := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		name := strings.TrimSpace(item)
		if !wgconfig.ValidInterfaceName(name) {
			return nil, false, fmt.Errorf("invalid WireGuard interface name %q", name)
		}
		selected[name] = true
	}
	return selected, false, nil
}

func dashToEmpty(value string) string {
	if value == "(none)" || value == "off" {
		return ""
	}
	return value
}

func splitComma(value string) []string {
	if value == "" || value == "(none)" {
		return []string{}
	}
	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

type ipAddressInfo struct {
	Interface string `json:"ifname"`
	Addresses []struct {
		Local     string `json:"local"`
		PrefixLen int    `json:"prefixlen"`
	} `json:"addr_info"`
}

func mergeAddressMetadata(interfaces []wireGuardInterfaceStatus, value []byte) error {
	var payload []ipAddressInfo
	if err := json.Unmarshal(value, &payload); err != nil {
		return err
	}
	byName := map[string][]string{}
	for _, item := range payload {
		for _, address := range item.Addresses {
			if address.Local != "" {
				byName[item.Interface] = append(byName[item.Interface], fmt.Sprintf("%s/%d", address.Local, address.PrefixLen))
			}
		}
	}
	for index := range interfaces {
		if addresses, ok := byName[interfaces[index].Name]; ok {
			interfaces[index].Addresses = addresses
		}
	}
	return nil
}

type ipLinkInfo struct {
	Interface string   `json:"ifname"`
	MTU       int      `json:"mtu"`
	State     string   `json:"operstate"`
	Flags     []string `json:"flags"`
}

func mergeLinkMetadata(interfaces []wireGuardInterfaceStatus, value []byte) error {
	var payload []ipLinkInfo
	if err := json.Unmarshal(value, &payload); err != nil {
		return err
	}
	byName := map[string]ipLinkInfo{}
	for _, item := range payload {
		byName[item.Interface] = item
	}
	for index := range interfaces {
		if item, ok := byName[interfaces[index].Name]; ok {
			interfaces[index].MTU = item.MTU
			interfaces[index].Up = strings.EqualFold(item.State, "UP") || containsString(item.Flags, "UP")
		}
	}
	return nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

type wireGuardManager struct {
	runner    commandRunner
	configDir string
}

func managedInterfaceName(networkID string) string {
	digest := sha256.Sum256([]byte(networkID))
	return "wm" + hex.EncodeToString(digest[:])[:10]
}

func normalizedInterfaceAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.String(), nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return "", err
	}
	if address.Is4() {
		return address.String() + "/32", nil
	}
	return address.String() + "/128", nil
}

func connectivityCheck(parent context.Context, selector string, runner commandRunner) (string, error) {
	interfaces, collectionError := collectWireGuard(parent, selector, runner)
	if len(interfaces) == 0 {
		if collectionError == "" {
			collectionError = "no WireGuard interfaces found"
		}
		return "", errors.New(collectionError)
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	peerCount, recentHandshakes, pingOK, pingFailed := 0, 0, 0, 0
	seen := map[string]bool{}
	for _, iface := range interfaces {
		for _, peer := range iface.Peers {
			peerCount++
			if value, err := time.Parse(time.RFC3339, peer.LatestHandshakeAt); err == nil && time.Since(value) <= 3*time.Minute {
				recentHandshakes++
			}
			for _, allowed := range peer.AllowedIPs {
				prefix, err := netip.ParsePrefix(allowed)
				if err != nil || !prefix.Addr().Is4() {
					continue
				}
				target := prefix.Addr().String()
				if seen[target] || len(seen) >= 8 {
					continue
				}
				seen[target] = true
				if _, err := runner.Run(ctx, "ping", "-c", "1", "-W", "2", target); err == nil {
					pingOK++
				} else {
					pingFailed++
				}
			}
		}
	}
	message := fmt.Sprintf("interfaces=%d peers=%d recent_handshakes=%d ping_ok=%d ping_failed=%d", len(interfaces), peerCount, recentHandshakes, pingOK, pingFailed)
	if collectionError != "" {
		message += " warning=" + collectionError
	}
	if peerCount > 0 && recentHandshakes == 0 && pingOK == 0 {
		return message, errors.New("all peer connectivity checks failed")
	}
	return message, nil
}

func validateNodeConfig(config nodeConfig, expectedNodeID string) error {
	if config.NodeID == "" || config.NodeID != expectedNodeID {
		return errors.New("configuration node identity does not match this agent")
	}
	if strings.TrimSpace(config.NetworkID) == "" {
		return errors.New("configuration network is required")
	}
	if _, err := normalizedInterfaceAddress(config.Address); err != nil {
		return errors.New("configuration address is invalid")
	}
	if config.ListenPort < 1 || config.ListenPort > 65535 {
		return errors.New("configuration listen port is invalid")
	}
	if config.MTU < 576 || config.MTU > 9000 {
		return errors.New("configuration MTU is invalid")
	}
	if err := wgconfig.ValidateKey(config.PrivateKey); err != nil {
		return errors.New("configuration private key is invalid")
	}
	seen := map[string]bool{}
	for _, peer := range config.Peers {
		if err := wgconfig.ValidateKey(peer.PublicKey); err != nil {
			return fmt.Errorf("peer %s has an invalid public key", peer.NodeID)
		}
		if seen[peer.PublicKey] {
			return fmt.Errorf("peer %s is duplicated", peer.NodeID)
		}
		seen[peer.PublicKey] = true
		if strings.ContainsAny(peer.Endpoint, "\r\n") {
			return fmt.Errorf("peer %s has an invalid endpoint", peer.NodeID)
		}
		if peer.Endpoint != "" {
			if _, _, err := net.SplitHostPort(peer.Endpoint); err != nil {
				return fmt.Errorf("peer %s has an invalid endpoint", peer.NodeID)
			}
		}
		if len(peer.AllowedIPs) == 0 {
			return fmt.Errorf("peer %s has no allowed IPs", peer.NodeID)
		}
		for _, allowed := range peer.AllowedIPs {
			if _, err := netip.ParsePrefix(allowed); err != nil {
				return fmt.Errorf("peer %s has an invalid allowed IP", peer.NodeID)
			}
		}
	}
	return nil
}

func renderWireGuardConfig(config nodeConfig) ([]byte, error) {
	if config.MTU == 0 {
		config.MTU = defaultAgentMTU
	}
	if err := validateNodeConfig(config, config.NodeID); err != nil {
		return nil, err
	}
	var output strings.Builder
	output.WriteString("[Interface]\nPrivateKey = ")
	output.WriteString(config.PrivateKey)
	output.WriteString("\nAddress = ")
	address, _ := normalizedInterfaceAddress(config.Address)
	output.WriteString(address)
	output.WriteString("\nListenPort = ")
	output.WriteString(strconv.Itoa(config.ListenPort))
	output.WriteString("\nMTU = ")
	output.WriteString(strconv.Itoa(config.MTU))
	output.WriteString("\n\n")
	for _, peer := range config.Peers {
		output.WriteString("[Peer]\nPublicKey = ")
		output.WriteString(peer.PublicKey)
		output.WriteString("\n")
		if peer.Endpoint != "" {
			output.WriteString("Endpoint = ")
			output.WriteString(peer.Endpoint)
			output.WriteString("\nPersistentKeepalive = 25\n")
		}
		output.WriteString("AllowedIPs = ")
		output.WriteString(strings.Join(peer.AllowedIPs, ", "))
		output.WriteString("\n\n")
	}
	return []byte(output.String()), nil
}

func (manager wireGuardManager) Apply(parent context.Context, config nodeConfig, expectedNodeID string) (string, string) {
	if config.MTU == 0 {
		config.MTU = defaultAgentMTU
	}
	if err := validateNodeConfig(config, expectedNodeID); err != nil {
		return "failed", err.Error()
	}
	content, err := renderWireGuardConfig(config)
	if err != nil {
		return "failed", err.Error()
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	if manager.runner == nil {
		manager.runner = execCommandRunner{}
	}
	if manager.configDir == "" {
		manager.configDir = "/etc/wireguard"
	}
	if err := os.MkdirAll(manager.configDir, 0o700); err != nil {
		return "failed", "create WireGuard configuration directory: " + err.Error()
	}
	interfaceName := managedInterfaceName(config.NetworkID)
	configurationPath := filepath.Join(manager.configDir, interfaceName+".conf")
	previous, readErr := os.ReadFile(configurationPath)
	previousExists := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return "failed", "read previous WireGuard configuration: " + readErr.Error()
	}
	stagedPath, err := stageConfiguration(manager.configDir, content)
	if err != nil {
		return "failed", "stage WireGuard configuration: " + err.Error()
	}
	defer os.Remove(stagedPath)

	_, interfaceErr := manager.runner.Run(ctx, "wg", "show", interfaceName)
	wasUp := interfaceErr == nil
	if wasUp {
		downTarget := interfaceName
		var previousDir string
		if previousExists {
			previousDir, err = os.MkdirTemp(manager.configDir, ".wiremesh-previous-")
			if err != nil {
				return "failed", "stage previous WireGuard configuration: " + err.Error()
			}
			defer os.RemoveAll(previousDir)
			downTarget = filepath.Join(previousDir, interfaceName+".conf")
			if err := os.WriteFile(downTarget, previous, 0o600); err != nil {
				return "failed", "stage previous WireGuard configuration: " + err.Error()
			}
		}
		if _, err := manager.runner.Run(ctx, "wg-quick", "down", downTarget); err != nil {
			return "failed", "stop current WireGuard interface: " + err.Error()
		}
	}
	if err := replaceFile(stagedPath, configurationPath); err != nil {
		if previousExists {
			_ = atomicWriteFile(configurationPath, previous, 0o600)
		}
		if wasUp && previousExists {
			_, _ = manager.runner.Run(ctx, "wg-quick", "up", configurationPath)
		}
		return "failed", "activate WireGuard configuration file: " + err.Error()
	}
	if _, err := manager.runner.Run(ctx, "wg-quick", "up", configurationPath); err == nil {
		return "applied", "WireGuard configuration applied to " + interfaceName
	} else {
		applyErr := err
		if _, downErr := manager.runner.Run(ctx, "wg-quick", "down", configurationPath); downErr != nil {
			_, _ = manager.runner.Run(ctx, "ip", "link", "delete", "dev", interfaceName)
		}
		if !previousExists {
			_ = os.Remove(configurationPath)
			return "failed", "apply WireGuard configuration: " + applyErr.Error()
		}
		if restoreErr := atomicWriteFile(configurationPath, previous, 0o600); restoreErr != nil {
			return "failed", "apply failed and restore file failed: " + restoreErr.Error()
		}
		if _, rollbackErr := manager.runner.Run(ctx, "wg-quick", "up", configurationPath); rollbackErr != nil {
			return "failed", "apply failed and rollback failed: " + rollbackErr.Error()
		}
		return "rolled_back", "new configuration failed; previous WireGuard configuration restored"
	}
}

func stageConfiguration(directory string, content []byte) (string, error) {
	file, err := os.CreateTemp(directory, ".wiremesh-new-*.conf")
	if err != nil {
		return "", err
	}
	name := file.Name()
	failed := true
	defer func() {
		_ = file.Close()
		if failed {
			_ = os.Remove(name)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := file.Write(content); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	failed = false
	return name, nil
}

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}

func atomicWriteFile(filename string, content []byte, mode os.FileMode) error {
	temporary, err := stageConfiguration(filepath.Dir(filename), content)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := os.Chmod(temporary, mode); err != nil {
		return err
	}
	return replaceFile(temporary, filename)
}
