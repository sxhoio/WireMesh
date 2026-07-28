package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/wgconfig"
)

type peerConfigFile struct {
	Interface string `json:"interface"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type peerConfigResponse struct {
	NodeID string           `json:"node_id"`
	Files  []peerConfigFile `json:"files"`
}

func collectPeerConfigFiles(selector, configDir string, observed []wireGuardInterfaceStatus) ([]peerConfigFile, string) {
	if configDir == "" {
		configDir = "/etc/wireguard"
	}
	names, err := peerConfigInterfaceNames(selector, configDir, observed)
	if err != nil {
		return []peerConfigFile{}, err.Error()
	}
	files := make([]peerConfigFile, 0, len(names))
	warnings := []string{}
	for _, name := range names {
		path := filepath.Join(configDir, name+".conf")
		content, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				warnings = append(warnings, fmt.Sprintf("read %s peer config: %v", name, err))
			}
			continue
		}
		info, _ := os.Stat(path)
		updatedAt := ""
		if info != nil {
			updatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
		files = append(files, peerConfigFile{
			Interface: name,
			Path:      path,
			Content:   extractPeerConfigSections(string(content)),
			UpdatedAt: updatedAt,
		})
	}
	if files == nil {
		files = []peerConfigFile{}
	}
	return files, strings.Join(warnings, "; ")
}

func peerConfigInterfaceNames(selector, configDir string, observed []wireGuardInterfaceStatus) ([]string, error) {
	selected, all, err := parseInterfaceSelector(selector)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	if all {
		for _, iface := range observed {
			if wgconfig.ValidInterfaceName(iface.Name) {
				names[iface.Name] = true
			}
		}
		if matches, err := filepath.Glob(filepath.Join(configDir, "*.conf")); err == nil {
			for _, match := range matches {
				name := strings.TrimSuffix(filepath.Base(match), ".conf")
				if wgconfig.ValidInterfaceName(name) {
					names[name] = true
				}
			}
		}
	} else {
		for name := range selected {
			names[name] = true
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func extractPeerConfigSections(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	scanner := bufio.NewScanner(strings.NewReader(content))
	lines := []string{}
	inPeer := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inPeer = strings.EqualFold(trimmed, "[Peer]")
		}
		if inPeer {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (manager wireGuardManager) ApplyPeerConfigFiles(parent context.Context, files []peerConfigFile) (string, error) {
	if len(files) == 0 {
		return "no peer config files", nil
	}
	if manager.runner == nil {
		manager.runner = execCommandRunner{}
	}
	if manager.configDir == "" {
		manager.configDir = "/etc/wireguard"
	}
	applied := 0
	unchanged := 0
	for _, file := range files {
		status, err := manager.applyPeerConfigFile(parent, file)
		if err != nil {
			return fmt.Sprintf("interface=%s status=failed message=%s", file.Interface, status), err
		}
		if status == "unchanged" {
			unchanged++
		} else {
			applied++
		}
	}
	return fmt.Sprintf("peer_config_files=%d applied=%d unchanged=%d", len(files), applied, unchanged), nil
}

func (manager wireGuardManager) applyPeerConfigFile(parent context.Context, file peerConfigFile) (string, error) {
	interfaceName := strings.TrimSpace(file.Interface)
	if !wgconfig.ValidInterfaceName(interfaceName) {
		return "invalid interface", fmt.Errorf("invalid WireGuard interface name %q", interfaceName)
	}
	content := wgconfig.NormalizePeerConfig(file.Content)
	if err := wgconfig.ValidatePeerConfig(content); err != nil {
		return err.Error(), err
	}
	if err := os.MkdirAll(manager.configDir, 0o700); err != nil {
		return "create WireGuard configuration directory: " + err.Error(), err
	}
	configurationPath := filepath.Join(manager.configDir, interfaceName+".conf")
	previous, err := os.ReadFile(configurationPath)
	if err != nil {
		return "read existing WireGuard configuration: " + err.Error(), fmt.Errorf("read existing WireGuard configuration: %w", err)
	}
	next, err := replacePeerConfigSections(string(previous), content)
	if err != nil {
		return err.Error(), err
	}
	if bytes.Equal(previous, next) {
		return "unchanged", nil
	}
	if err := manager.replaceAndRestart(parent, interfaceName, configurationPath, previous, next); err != nil {
		return err.Error(), err
	}
	return "applied", nil
}

func (manager wireGuardManager) replaceAndRestart(parent context.Context, interfaceName, configurationPath string, previous, next []byte) error {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	stagedPath, err := stageConfiguration(manager.configDir, next)
	if err != nil {
		return fmt.Errorf("stage peer configuration: %w", err)
	}
	defer os.Remove(stagedPath)

	_, interfaceErr := manager.runner.Run(ctx, "wg", "show", interfaceName)
	wasUp := interfaceErr == nil
	if wasUp {
		if _, err := manager.runner.Run(ctx, "wg-quick", "down", configurationPath); err != nil {
			return fmt.Errorf("stop current WireGuard interface: %w", err)
		}
	}
	if err := replaceFile(stagedPath, configurationPath); err != nil {
		if wasUp {
			_, _ = manager.runner.Run(ctx, "wg-quick", "up", configurationPath)
		}
		return fmt.Errorf("activate peer configuration file: %w", err)
	}
	if _, err := manager.runner.Run(ctx, "wg-quick", "up", configurationPath); err == nil {
		return nil
	} else {
		applyErr := err
		_, _ = manager.runner.Run(ctx, "wg-quick", "down", configurationPath)
		if restoreErr := atomicWriteFile(configurationPath, previous, 0o600); restoreErr != nil {
			return fmt.Errorf("apply failed and restore file failed: %w", restoreErr)
		}
		if wasUp {
			if _, rollbackErr := manager.runner.Run(ctx, "wg-quick", "up", configurationPath); rollbackErr != nil {
				return fmt.Errorf("apply failed and rollback failed: %w", rollbackErr)
			}
		}
		return fmt.Errorf("apply peer configuration: %w", applyErr)
	}
}

func replacePeerConfigSections(fullConfig, peerContent string) ([]byte, error) {
	fullConfig = strings.ReplaceAll(fullConfig, "\r\n", "\n")
	fullConfig = strings.ReplaceAll(fullConfig, "\r", "\n")
	if !strings.Contains(strings.ToLower(fullConfig), "[interface]") {
		return nil, errors.New("existing WireGuard configuration is missing [Interface]")
	}
	scanner := bufio.NewScanner(strings.NewReader(fullConfig))
	prefix := []string{}
	inPeer := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if strings.EqualFold(trimmed, "[Peer]") {
				inPeer = true
			} else if inPeer {
				return nil, fmt.Errorf("unsupported section %s after [Peer]", trimmed)
			}
		}
		if inPeer {
			continue
		}
		prefix = append(prefix, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.Join(prefix, "\n"), "\n")
	peerContent = wgconfig.NormalizePeerConfig(peerContent)
	if peerContent == "" {
		return []byte(base + "\n"), nil
	}
	return []byte(base + "\n\n" + peerContent + "\n"), nil
}
