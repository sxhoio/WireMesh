package wgconfig

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

func NormalizePeerConfig(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimSpace(content)
}

func PeerConfigContainsSecretOrInterface(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "[interface]") || strings.Contains(lower, "privatekey")
}

func ValidatePeerConfig(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if PeerConfigContainsSecretOrInterface(content) {
		return errors.New("peer config must only contain [Peer] sections and must not contain Interface or PrivateKey")
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
				return errors.New("peer config must only contain [Peer] sections")
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
			return errors.New("peer config must start with a [Peer] section")
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return fmt.Errorf("invalid peer config line %q", trimmed)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "publickey":
			if err := ValidateKey(value); err != nil {
				return fmt.Errorf("peer %d has an invalid PublicKey", peerIndex)
			}
			hasPublicKey = true
		case "presharedkey":
			if err := ValidateKey(value); err != nil {
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

func ValidateKey(value string) error {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return errors.New("invalid WireGuard key")
	}
	return nil
}

func ValidInterfaceName(value string) bool {
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
