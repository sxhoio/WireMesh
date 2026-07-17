package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type testRunner struct {
	calls []string
	run   func(string, ...string) ([]byte, error)
}

func (runner *testRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, name+" "+strings.Join(args, " "))
	return runner.run(name, args...)
}

func testWireGuardKey(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytesOf(fill, 32))
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func TestParseWireGuardDumpDoesNotExposeSecrets(t *testing.T) {
	privateKey := testWireGuardKey(1)
	publicKey := testWireGuardKey(2)
	peerKey := testWireGuardKey(3)
	presharedKey := testWireGuardKey(4)
	dump := strings.Join([]string{
		"wg0\t" + privateKey + "\t" + publicKey + "\t51820\toff",
		"wg0\t" + peerKey + "\t" + presharedKey + "\t198.51.100.10:51820\t10.20.0.2/32\t1710000000\t1234\t5678\t25",
	}, "\n")

	interfaces, err := parseWireGuardDump(dump, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 1 || interfaces[0].Name != "wg0" || interfaces[0].PublicKey != publicKey {
		t.Fatalf("unexpected interfaces: %#v", interfaces)
	}
	if len(interfaces[0].Peers) != 1 || interfaces[0].Peers[0].ReceiveBytes != 1234 || interfaces[0].Peers[0].TransmitBytes != 5678 {
		t.Fatalf("unexpected peers: %#v", interfaces[0].Peers)
	}
	encoded, err := json.Marshal(interfaces)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), privateKey) || strings.Contains(string(encoded), presharedKey) {
		t.Fatalf("private material was exposed in status JSON: %s", encoded)
	}
}

func TestParseWireGuardDumpSupportsMultiplePeersWithDisabledKeepalive(t *testing.T) {
	interfaceKey := testWireGuardKey(10)
	firstPeerKey := testWireGuardKey(11)
	secondPeerKey := testWireGuardKey(12)
	dump := strings.Join([]string{
		"wg0\tprivate\t" + interfaceKey + "\t11011\toff",
		"wg0\t" + firstPeerKey + "\t(none)\t61.242.135.141:32413\t10.88.88.88/32\t1710000000\t264130\t107008\toff",
		"wg0\t" + secondPeerKey + "\t(none)\t[2402:4e00:1420:800:9c90:f48:88c0:1]:38958\t10.88.88.5/32,10.88.90.0/24\t1710000010\t42184171\t4162846\t25",
	}, "\n")

	interfaces, err := parseWireGuardDump(dump, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 1 || len(interfaces[0].Peers) != 2 {
		t.Fatalf("unexpected multi-peer interfaces: %#v", interfaces)
	}
	if interfaces[0].Peers[0].PersistentKeepalive != 0 {
		t.Fatalf("disabled keepalive should be reported as zero: %#v", interfaces[0].Peers[0])
	}
	if interfaces[0].Peers[1].PersistentKeepalive != 25 {
		t.Fatalf("numeric keepalive was not preserved: %#v", interfaces[0].Peers[1])
	}
	if len(interfaces[0].Peers[1].AllowedIPs) != 2 {
		t.Fatalf("multiple allowed IPs were not preserved: %#v", interfaces[0].Peers[1].AllowedIPs)
	}
}

func TestParsePersistentKeepaliveRejectsInvalidValue(t *testing.T) {
	if _, err := parsePersistentKeepalive("invalid"); err == nil {
		t.Fatal("expected invalid keepalive value to be rejected")
	}
}

func TestCollectWireGuardMergesInterfaceMetadata(t *testing.T) {
	runner := &testRunner{run: func(name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "wg show all dump":
			return []byte("wg0\tprivate\tpublic\t51820\toff\n"), nil
		case "ip -j address show":
			return []byte("[{\"ifname\":\"wg0\",\"addr_info\":[{\"local\":\"10.0.0.1\",\"prefixlen\":24}]}]"), nil
		case "ip -j link show":
			return []byte("[{\"ifname\":\"wg0\",\"mtu\":1420,\"operstate\":\"UNKNOWN\",\"flags\":[\"POINTOPOINT\",\"UP\"]}]"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}}
	interfaces, warning := collectWireGuard(context.Background(), "wg0", runner)
	if warning != "" {
		t.Fatalf("unexpected collection warning: %s", warning)
	}
	if len(interfaces) != 1 || interfaces[0].MTU != 1420 || !interfaces[0].Up || interfaces[0].Addresses[0] != "10.0.0.1/24" {
		t.Fatalf("metadata was not merged: %#v", interfaces)
	}
}

func TestRenderAndApplyWireGuardConfiguration(t *testing.T) {
	config := nodeConfig{
		NodeID: "node-1", NetworkID: "network-1", Address: "10.44.0.1/32",
		PrivateKey: testWireGuardKey(5), ListenPort: 51820,
		Peers: []peerConfig{{
			NodeID: "node-2", PublicKey: testWireGuardKey(6), Endpoint: "vpn.example.com:51820",
			AllowedIPs: []string{"10.44.0.2/32"},
		}},
	}
	runner := &testRunner{run: func(name string, args ...string) ([]byte, error) {
		if name == "wg" && len(args) > 0 && args[0] == "show" {
			return nil, errors.New("interface does not exist")
		}
		if name == "wg-quick" && len(args) > 0 && args[0] == "up" {
			return nil, nil
		}
		return nil, errors.New("unexpected command")
	}}
	directory := t.TempDir()
	status, message := (wireGuardManager{runner: runner, configDir: directory}).Apply(context.Background(), config, "node-1")
	if status != "applied" || !strings.Contains(message, managedInterfaceName(config.NetworkID)) {
		t.Fatalf("unexpected apply result: %s %s", status, message)
	}
	filename := filepath.Join(directory, managedInterfaceName(config.NetworkID)+".conf")
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, expected := range []string{"[Interface]", "Address = 10.44.0.1/32", "MTU = 1420", "[Peer]", "AllowedIPs = 10.44.0.2/32", "PersistentKeepalive = 25"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("configuration is missing %q: %s", expected, text)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filename); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("configuration permissions are not 0600: %v %v", info, err)
		}
	}
}

func TestApplyWireGuardConfigurationRollsBack(t *testing.T) {
	config := nodeConfig{
		NodeID: "node-1", NetworkID: "network-rollback", Address: "10.45.0.1",
		PrivateKey: testWireGuardKey(7), ListenPort: 51820,
	}
	directory := t.TempDir()
	filename := filepath.Join(directory, managedInterfaceName(config.NetworkID)+".conf")
	previous := []byte("[Interface]\nPrivateKey = previous\nAddress = 10.45.0.1/32\n")
	if err := os.WriteFile(filename, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	upCount := 0
	runner := &testRunner{run: func(name string, args ...string) ([]byte, error) {
		if name == "wg" && args[0] == "show" {
			return nil, nil
		}
		if name == "wg-quick" && args[0] == "down" {
			return nil, nil
		}
		if name == "wg-quick" && args[0] == "up" {
			upCount++
			if upCount == 1 {
				return nil, errors.New("new configuration rejected")
			}
			return nil, nil
		}
		return nil, errors.New("unexpected command")
	}}
	status, _ := (wireGuardManager{runner: runner, configDir: directory}).Apply(context.Background(), config, "node-1")
	if status != "rolled_back" {
		t.Fatalf("expected rollback, got %s", status)
	}
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(previous) {
		t.Fatalf("previous configuration was not restored: %s", restored)
	}
}

func TestValidateNodeConfigRejectsWrongNode(t *testing.T) {
	config := nodeConfig{NodeID: "another-node", NetworkID: "network-1", Address: "10.0.0.1/32", PrivateKey: testWireGuardKey(1), ListenPort: 51820}
	if err := validateNodeConfig(config, "this-node"); err == nil {
		t.Fatal("expected node identity mismatch")
	}
}
