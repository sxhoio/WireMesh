package wgconfig

import (
	"strings"
	"testing"
)

const testWireGuardKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestNormalizePeerConfig(t *testing.T) {
	got := NormalizePeerConfig("\r\n[Peer]\r\nPublicKey = key\r\n")
	if got != "[Peer]\nPublicKey = key" {
		t.Fatalf("NormalizePeerConfig() = %q", got)
	}
}

func TestValidatePeerConfigAcceptsPeerSections(t *testing.T) {
	content := strings.Join([]string{
		"[Peer]",
		"PublicKey = " + testWireGuardKey,
		"PresharedKey = " + testWireGuardKey,
		"AllowedIPs = 10.0.0.2/32, fd00::2/128",
		"Endpoint = example.com:51820",
		"PersistentKeepalive = 25",
		"",
		"[Peer]",
		"PublicKey = " + testWireGuardKey,
		"AllowedIPs = 10.0.0.3/32",
	}, "\n")

	if err := ValidatePeerConfig(content); err != nil {
		t.Fatalf("ValidatePeerConfig() error = %v", err)
	}
}

func TestValidatePeerConfigRejectsSecretOrInterface(t *testing.T) {
	content := strings.Join([]string{
		"[Peer]",
		"PublicKey = " + testWireGuardKey,
		"PrivateKey = " + testWireGuardKey,
		"AllowedIPs = 10.0.0.2/32",
	}, "\n")

	err := ValidatePeerConfig(content)
	if err == nil || err.Error() != "peer config must only contain [Peer] sections and must not contain Interface or PrivateKey" {
		t.Fatalf("ValidatePeerConfig() error = %v", err)
	}
	if !PeerConfigContainsSecretOrInterface("[Interface]\nPrivateKey = x") {
		t.Fatal("PeerConfigContainsSecretOrInterface() did not detect forbidden content")
	}
}

func TestValidatePeerConfigRequiresPeerFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "missing public key",
			content: strings.Join([]string{
				"[Peer]",
				"AllowedIPs = 10.0.0.2/32",
			}, "\n"),
			want: "peer 1 is missing PublicKey",
		},
		{
			name: "missing allowed ips",
			content: strings.Join([]string{
				"[Peer]",
				"PublicKey = " + testWireGuardKey,
			}, "\n"),
			want: "peer 1 is missing AllowedIPs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePeerConfig(tt.content)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("ValidatePeerConfig() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidatePeerConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "must start with peer",
			content: "PublicKey = " + testWireGuardKey,
			want:    "peer config must start with a [Peer] section",
		},
		{
			name:    "only peer sections",
			content: "[Other]",
			want:    "peer config must only contain [Peer] sections",
		},
		{
			name: "invalid public key",
			content: strings.Join([]string{
				"[Peer]",
				"PublicKey = bad",
				"AllowedIPs = 10.0.0.2/32",
			}, "\n"),
			want: "peer 1 has an invalid PublicKey",
		},
		{
			name: "invalid allowed ips",
			content: strings.Join([]string{
				"[Peer]",
				"PublicKey = " + testWireGuardKey,
				"AllowedIPs = not-a-prefix",
			}, "\n"),
			want: "peer 1 has an invalid AllowedIPs entry",
		},
		{
			name: "invalid keepalive",
			content: strings.Join([]string{
				"[Peer]",
				"PublicKey = " + testWireGuardKey,
				"AllowedIPs = 10.0.0.2/32",
				"PersistentKeepalive = 65536",
			}, "\n"),
			want: "peer 1 has an invalid PersistentKeepalive",
		},
		{
			name: "unsupported key",
			content: strings.Join([]string{
				"[Peer]",
				"PublicKey = " + testWireGuardKey,
				"AllowedIPs = 10.0.0.2/32",
				"Foo = bar",
			}, "\n"),
			want: `unsupported peer config key "Foo"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePeerConfig(tt.content)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("ValidatePeerConfig() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateKey(t *testing.T) {
	if err := ValidateKey(testWireGuardKey); err != nil {
		t.Fatalf("ValidateKey() error = %v", err)
	}
	if err := ValidateKey("bad"); err == nil || err.Error() != "invalid WireGuard key" {
		t.Fatalf("ValidateKey() error = %v", err)
	}
}

func TestValidInterfaceName(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "wg0", want: true},
		{value: "wm.test-1", want: true},
		{value: "", want: false},
		{value: "this-name-is-too-long", want: false},
		{value: "bad/name", want: false},
		{value: "中文", want: false},
	}

	for _, tt := range tests {
		if got := ValidInterfaceName(tt.value); got != tt.want {
			t.Fatalf("ValidInterfaceName(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}
