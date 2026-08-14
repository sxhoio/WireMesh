package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

func TestUpdateManifestSigningRoundtrip(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := wireproto.AgentUpdateManifest{
		Available: true, Version: "0.4.26", OS: "linux", Arch: "amd64",
		Size: 12345, SHA256: "abc123", MinAgentVersion: "0.3.6", CurrentCompatible: true,
	}
	signature, err := signUpdateManifest(key, manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = signature
	if err := verifyUpdateManifestSignature(&key.PublicKey, manifest); err != nil {
		t.Fatalf("valid signature must verify: %v", err)
	}

	// 篡改 SHA256 后验签失败
	tampered := manifest
	tampered.SHA256 = "tampered"
	if err := verifyUpdateManifestSignature(&key.PublicKey, tampered); err == nil {
		t.Fatal("tampered manifest must fail verification")
	}
	// 缺少签名必须失败
	unsigned := manifest
	unsigned.Signature = ""
	if err := verifyUpdateManifestSignature(&key.PublicKey, unsigned); err == nil {
		t.Fatal("unsigned manifest must fail verification when a key is configured")
	}
	// 错误公钥必须失败
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err := verifyUpdateManifestSignature(&other.PublicKey, manifest); err == nil {
		t.Fatal("wrong public key must fail verification")
	}
}

func TestUpdateSigningKeyPEMParsing(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	parsed, err := parseECDSAPrivateKeyPEM(pemText)
	if err != nil {
		t.Fatalf("parse signing key: %v", err)
	}
	// 签名/验签闭环
	manifest := wireproto.AgentUpdateManifest{Version: "1", OS: "linux", Arch: "arm64", Size: 1, SHA256: "x"}
	signature, err := signUpdateManifest(parsed, manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = signature
	if err := verifyUpdateManifestSignature(&parsed.PublicKey, manifest); err != nil {
		t.Fatalf("roundtrip verify: %v", err)
	}
}
